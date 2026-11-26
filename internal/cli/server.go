package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/McMelonTV/check-usage/internal/nativecli"
	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/internal/remote"
	"github.com/McMelonTV/check-usage/internal/remoteserver"
	"github.com/McMelonTV/check-usage/usageapi"
	"github.com/charmbracelet/x/term"
)

func runServerCommand(args []string) int {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	setDoubleDashFlagUsage(fs)
	configPath := fs.String("config", remoteserver.DefaultConfigPath(), "server configuration file")
	if fs.Parse(args) != nil {
		return 2
	}
	if fs.NArg() == 0 {
		printServerCommandUsage()
		return 2
	}
	command, rest := fs.Arg(0), fs.Args()[1:]
	switch command {
	case "help", "-h", "--help":
		printServerCommandUsage()
		return 0
	case "setup":
		return runServerSetup(*configPath, rest)
	case "init":
		if len(rest) != 0 {
			return 2
		}
		_, err := remoteserver.Initialize(*configPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		fmt.Println("Created server configuration:", *configPath)
		return 0
	case "service":
		if _, created, err := remoteserver.EnsureInitialized(*configPath); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		} else if created {
			fmt.Fprintln(os.Stderr, "Initialized server configuration:", *configPath)
		}
		return runServerServiceCommand(*configPath, rest)
	}

	config, created, err := remoteserver.EnsureInitialized(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if created {
		fmt.Fprintln(os.Stderr, "Initialized server configuration:", *configPath)
	}

	switch command {
	case "serve":
		if len(rest) != 0 {
			return 2
		}
		service := usageapi.New(usageapi.Config{AccountsFile: config.AccountsFile, CacheDir: config.CacheDir, MinRefreshInterval: time.Minute})
		handler := &remoteserver.Handler{Config: config, Service: service}
		server := &http.Server{Addr: config.Listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 3 * time.Minute, IdleTimeout: time.Minute, MaxHeaderBytes: 16 << 10}
		signals, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stopSignals()
		ctx, cancel := context.WithCancel(signals)
		defer cancel()
		server.BaseContext = func(net.Listener) context.Context { return ctx }
		if binary := os.Getenv(serviceBinaryEnv); binary != "" {
			go watchServiceBinary(ctx, binary, serviceBinaryPollInterval, func() {
				fmt.Println("check-usage was upgraded; restarting on the new binary")
				cancel()
			})
		}
		go func() {
			<-ctx.Done()
			shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			_ = server.Shutdown(shutdown)
		}()
		fmt.Println("Usage server listening on", config.Listen)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		return 0
	case "pair":
		return runServerPair(*configPath, config, rest)
	case "devices":
		return runServerDevices(config, rest)
	case "api":
		return runAPICommand(append([]string{"--accounts-file", config.AccountsFile, "--cache-dir", config.CacheDir}, rest...))
	case "accounts":
		return runServerAccounts(config, rest)
	default:
		fmt.Fprintln(os.Stderr, "unknown server command:", command)
		printServerCommandUsage()
		return 2
	}
}

func runServerPair(configPath string, config remoteserver.Config, args []string) int {
	// Allow optional positional device name before or after flags, e.g.:
	//   check-usage server pair laptop --allow-account-changes
	//   check-usage server pair --name laptop
	var positionalName string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		positionalName = args[0]
		args = args[1:]
	}

	pairFlags := flag.NewFlagSet("server pair", flag.ContinueOnError)
	pairFlags.SetOutput(os.Stderr)
	setDoubleDashFlagUsage(pairFlags)
	name := pairFlags.String("name", "", "device name (e.g. phone, laptop)")
	write := pairFlags.Bool("allow-account-changes", false, "allow this device to rename/remove shared accounts")
	serverURL := pairFlags.String("url", "", "server HTTPS origin to embed in the pairing link and QR code")
	out := pairFlags.String("out", "", "write one-time pairing code to a private file")
	noQR := pairFlags.Bool("no-qr", false, "omit the terminal QR code")
	codeOnly := pairFlags.Bool("code-only", false, "print only the raw pairing code")
	if pairFlags.Parse(args) != nil {
		return 2
	}
	if pairFlags.NArg() > 1 || (pairFlags.NArg() == 1 && (positionalName != "" || *name != "")) {
		fmt.Fprintln(os.Stderr, "usage: check-usage server pair [device-name] [--allow-account-changes] [--url https://...] [--no-qr] [--code-only] [--out path]")
		return 2
	}
	if pairFlags.NArg() == 1 {
		positionalName = pairFlags.Arg(0)
	}
	deviceName := strings.TrimSpace(*name)
	if deviceName == "" {
		deviceName = strings.TrimSpace(positionalName)
	}
	if deviceName == "" {
		if term.IsTerminal(os.Stdin.Fd()) && *out == "" && !*codeOnly {
			entered, err := promptLine("Device name (e.g. phone, laptop)", "device")
			if err != nil {
				return 1
			}
			deviceName = entered
		} else {
			deviceName = "device"
		}
	}

	resolvedURL := strings.TrimSpace(*serverURL)
	if resolvedURL != "" {
		valid, err := remote.ValidateURL(resolvedURL)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 2
		}
		resolvedURL = valid
		if config.PublicURL != resolvedURL {
			config.PublicURL = resolvedURL
			_ = remoteserver.SaveConfig(configPath, config)
		}
	}

	pairing, err := remoteserver.CreatePairing(config, deviceName, *write, time.Now())
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if *out != "" {
		file, err := os.OpenFile(*out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error: cannot create pairing code file")
			return 1
		}
		_, err = file.WriteString(pairing.Code + "\n")
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			return 1
		}
		fmt.Println("Pairing code written to", *out)
		fmt.Fprintln(os.Stderr, "Pairing expires in ten minutes and can be used once.")
		return 0
	}
	if *codeOnly || !term.IsTerminal(os.Stdout.Fd()) {
		fmt.Println(pairing.Code)
		fmt.Fprintln(os.Stderr, "Pairing expires in ten minutes and can be used once.")
		return 0
	}
	if resolvedURL == "" {
		resolvedURL = detectServerPublicURL(config)
	}
	printPairingOutput(resolvedURL, pairing.Code, !*noQR)
	return 0
}

func runServerDevices(config remoteserver.Config, rest []string) int {
	if len(rest) >= 1 && rest[0] == "revoke" {
		if len(rest) != 2 {
			fmt.Fprintln(os.Stderr, "usage: check-usage server devices revoke <device-id-or-name>")
			return 2
		}
		if err := remoteserver.RevokeDevice(config, rest[1]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		fmt.Println("Device revoked.")
		return 0
	}

	listArgs := rest
	if len(listArgs) > 0 && listArgs[0] == "list" {
		listArgs = listArgs[1:]
	}
	fs := flag.NewFlagSet("server devices list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	setDoubleDashFlagUsage(fs)
	asJSON := fs.Bool("json", false, "output devices as JSON")
	if fs.Parse(listArgs) != nil || fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: check-usage server devices [list [--json] | revoke <device-id-or-name>]")
		return 2
	}
	devices, err := remoteserver.ListDevices(config)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if *asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(devices)
		return 0
	}
	if len(devices) == 0 {
		fmt.Println("No paired devices yet. Run `check-usage server pair <name>` to pair one.")
		return 0
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tACCESS\tPAIRED\tID")
	for _, device := range devices {
		paired := "-"
		if device.CreatedAt > 0 {
			paired = time.Unix(device.CreatedAt, 0).Local().Format("2006-01-02 15:04")
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", device.Name, device.Access, paired, device.ID)
	}
	_ = w.Flush()
	return 0
}

func runServerAccounts(config remoteserver.Config, rest []string) int {
	if len(rest) == 0 {
		printAccountsCommandUsage()
		return 2
	}
	if rest[0] == "native" {
		return runNativeAccount(config, rest[1:])
	}
	if rest[0] == "add" {
		if code, handled := runSmartServerAccountsAdd(config, rest[1:]); handled {
			return code
		}
	}
	// Existing provider sign-in and API-key administration run locally on the server.
	localArgs := append([]string{rest[0], "--accounts-file", config.AccountsFile}, rest[1:]...)
	return runAccountsCommand(localArgs)
}

// runSmartServerAccountsAdd lets `check-usage server accounts add` work cleanly
// for any provider (interactive menu with no flags, or routing native/OAuth
// providers automatically when invoked interactively).
func runSmartServerAccountsAdd(config remoteserver.Config, args []string) (int, bool) {
	if len(args) == 0 && term.IsTerminal(os.Stdin.Fd()) {
		fmt.Println("Choose a provider to add:")
		fmt.Println("  1) Antigravity (native agy CLI)")
		fmt.Println("  2) Codex       (native codex CLI or OAuth)")
		fmt.Println("  3) Claude      (browser code)")
		fmt.Println("  4) Cursor      (browser login or API key)")
		fmt.Println("  5) OpenCode Go (API key)")
		fmt.Println("  6) DeepSeek    (API key)")
		choice, err := promptLine("Choose [1-6]", "")
		if err != nil || choice == "" {
			return 0, true
		}
		switch strings.ToLower(strings.TrimSpace(choice)) {
		case "1", "antigravity", "agy":
			if err := registerNativeInteractive(config, providers.Antigravity, "agy", "Google"); err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				return 1, true
			}
			return 0, true
		case "2", "codex":
			useNative, err := promptYesNo("Use local native codex CLI? (No = OAuth login)", true)
			if err != nil {
				return 1, true
			}
			if useNative {
				if err := registerNativeInteractive(config, providers.Codex, "codex", "OpenAI"); err != nil {
					fmt.Fprintln(os.Stderr, "error:", err)
					return 1, true
				}
				return 0, true
			}
			name, _ := promptLine("Display name (optional)", "")
			loginArgs := []string{"--accounts-file", config.AccountsFile, "--provider", "codex"}
			if name != "" {
				loginArgs = append(loginArgs, "--name", name)
			}
			return runAccountsLogin(loginArgs), true
		case "3", "claude":
			name, _ := promptLine("Display name (optional)", "")
			loginArgs := []string{"--accounts-file", config.AccountsFile, "--provider", "claude"}
			if name != "" {
				loginArgs = append(loginArgs, "--name", name)
			}
			return runAccountsLogin(loginArgs), true
		case "4", "cursor":
			useBrowser, err := promptYesNo("Sign in with Cursor browser login? (No = API key)", true)
			if err != nil {
				return 1, true
			}
			if useBrowser {
				name, _ := promptLine("Display name (optional)", "")
				loginArgs := []string{"--accounts-file", config.AccountsFile, "--provider", "cursor"}
				if name != "" {
					loginArgs = append(loginArgs, "--name", name)
				}
				return runAccountsLogin(loginArgs), true
			}
			if err := addAPIKeyInteractive(config.AccountsFile, providers.Cursor, "Cursor"); err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				return 1, true
			}
			return 0, true
		case "5", "opencode-go", "opencode":
			if err := addAPIKeyInteractive(config.AccountsFile, providers.OpenCodeGo, "OpenCode Go"); err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				return 1, true
			}
			return 0, true
		case "6", "deepseek":
			if err := addAPIKeyInteractive(config.AccountsFile, providers.DeepSeek, "DeepSeek"); err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				return 1, true
			}
			return 0, true
		default:
			fmt.Fprintln(os.Stderr, "error: unknown provider choice")
			return 2, true
		}
	}
	return 0, false
}

func runNativeAccount(server remoteserver.Config, args []string) int {
	fs := flag.NewFlagSet("server accounts native", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	setDoubleDashFlagUsage(fs)
	provider := fs.String("provider", "", "provider ID")
	name := fs.String("name", "", "account display name")
	account := fs.String("account", "", "attach to an existing server account")
	driver := fs.String("driver", "", "native driver: agy, codex, or json")
	executable := fs.String("executable", "", "native executable or adapter path")
	source := fs.String("source-file", "", "native adapter configuration JSON file")
	configDir := fs.String("config-dir", "", "Codex configuration directory for this profile")
	fallback := fs.Bool("fallback", false, "allow the existing account's API credentials as fallback")
	asJSON := fs.Bool("json", false, "output result as JSON")
	if fs.Parse(args) != nil || fs.NArg() != 0 {
		return 2
	}
	config := nativecli.Config{Driver: *driver, Executable: *executable, Fallback: *fallback}
	if *source != "" {
		data, err := os.ReadFile(*source)
		if err != nil || len(data) > 1<<20 || json.Unmarshal(data, &config) != nil {
			fmt.Fprintln(os.Stderr, "error: cannot read native adapter configuration")
			return 1
		}
	} else if *configDir != "" {
		if *driver != "codex" {
			fmt.Fprintln(os.Stderr, "error: --config-dir applies to Codex; other adapters use --source-file")
			return 2
		}
		absolute, err := filepath.Abs(*configDir)
		if err != nil {
			return 2
		}
		config.Env = map[string]string{"CODEX_HOME": absolute}
	}
	if *provider == "" {
		*provider = config.Driver
		if config.Driver == "agy" {
			*provider = "antigravity"
		}
	}
	if config.Fallback && strings.TrimSpace(*account) == "" {
		fmt.Fprintln(os.Stderr, "error: --fallback requires an existing --account")
		return 2
	}
	service := usageapi.New(usageapi.Config{AccountsFile: server.AccountsFile, CacheDir: server.CacheDir})
	result, err := service.SaveNativeAccount(usageapi.NativeAccount{Account: *account, Provider: *provider, Name: *name, Config: config})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if *asJSON || !term.IsTerminal(os.Stdout.Fd()) {
		_ = json.NewEncoder(os.Stdout).Encode(result)
		return 0
	}
	fmt.Printf("Saved native %s account %q (%s).\n", result.Account.Provider, result.Account.Name, result.Account.ID)
	return 0
}

func printServerCommandUsage() {
	fmt.Println("Usage:")
	fmt.Println("  check-usage server [--config path] setup")
	fmt.Println("  check-usage server [--config path] serve")
	fmt.Println("  check-usage server [--config path] pair [device-name] [--allow-account-changes] [--url https://...]")
	fmt.Println("  check-usage server [--config path] devices [list [--json] | revoke <id-or-name>]")
	fmt.Println("  check-usage server [--config path] accounts <add|list|native|login|reauth|rename|remove> [flags]")
	fmt.Println("  check-usage server [--config path] service <install|status|uninstall>")
	fmt.Println("  check-usage server [--config path] init")
	fmt.Println("  check-usage server [--config path] api [flags] <method|serve>")
}

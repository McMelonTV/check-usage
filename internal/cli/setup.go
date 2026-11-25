package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/McMelonTV/check-usage/internal/nativecli"
	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/internal/qr"
	"github.com/McMelonTV/check-usage/internal/remote"
	"github.com/McMelonTV/check-usage/internal/remoteserver"
	"github.com/McMelonTV/check-usage/internal/storage"
	"github.com/McMelonTV/check-usage/usageapi"
	"github.com/charmbracelet/x/term"
)

// readTTYLine reads one line directly from r without pre-buffering beyond '\n'
// so nested prompts (such as provider OAuth code entry) never lose input.
func readTTYLine(r io.Reader) (string, error) {
	var buf []byte
	var b [1]byte
	for {
		n, err := r.Read(b[:])
		if n > 0 {
			if b[0] == '\n' {
				break
			}
			if b[0] != '\r' {
				buf = append(buf, b[0])
			}
		}
		if err != nil {
			if err == io.EOF && len(buf) > 0 {
				return string(buf), nil
			}
			return string(buf), err
		}
	}
	return string(buf), nil
}

func promptLine(prompt, defaultVal string) (string, error) {
	if defaultVal != "" {
		fmt.Fprintf(os.Stderr, "%s [%s]: ", prompt, defaultVal)
	} else {
		fmt.Fprintf(os.Stderr, "%s: ", prompt)
	}
	line, err := readTTYLine(os.Stdin)
	if err != nil {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultVal, nil
	}
	return line, nil
}

func promptYesNo(prompt string, defaultYes bool) (bool, error) {
	hint := "[y/N]"
	if defaultYes {
		hint = "[Y/n]"
	}
	fmt.Fprintf(os.Stderr, "%s %s: ", prompt, hint)
	line, err := readTTYLine(os.Stdin)
	if err != nil {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	if answer == "" {
		return defaultYes, nil
	}
	return answer == "y" || answer == "yes", nil
}

func promptSecret(prompt string) (string, error) {
	fmt.Fprintf(os.Stderr, "%s: ", prompt)
	if term.IsTerminal(os.Stdin.Fd()) {
		secret, err := term.ReadPassword(os.Stdin.Fd())
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(secret)), nil
	}
	line, err := readTTYLine(os.Stdin)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func hasNativeProviderAccount(accounts []storage.Account, providerID, driver string) bool {
	for _, acc := range accounts {
		if acc.Provider == providerID && acc.AuthData.Native != nil && acc.AuthData.Native.Driver == driver {
			return true
		}
	}
	return false
}

func defaultAPIKeyEnvName(providerID string) string {
	switch providerID {
	case providers.OpenCodeGo:
		return "OPENCODE_API_KEY"
	case providers.DeepSeek:
		return "DEEPSEEK_API_KEY"
	case providers.Cursor:
		return "CURSOR_API_KEY"
	default:
		return ""
	}
}

func registerNativeInteractive(config remoteserver.Config, providerID, driver, defaultName string) error {
	name, err := promptLine("  Display name", defaultName)
	if err != nil {
		return err
	}
	service := usageapi.New(usageapi.Config{AccountsFile: config.AccountsFile, CacheDir: config.CacheDir})
	result, err := service.SaveNativeAccount(usageapi.NativeAccount{
		Provider: providerID,
		Name:     name,
		Config:   nativecli.Config{Driver: driver},
	})
	if err != nil {
		return err
	}
	fmt.Printf("  Registered %s (%s via native %s CLI).\n", result.Account.Name, providerID, driver)
	return nil
}

func addAPIKeyInteractive(accountsFile, providerID, defaultName string) error {
	name, err := promptLine("  Display name", defaultName)
	if err != nil {
		return err
	}
	envName := defaultAPIKeyEnvName(providerID)
	var apiKey string
	if envName != "" && strings.TrimSpace(os.Getenv(envName)) != "" {
		useEnv, err := promptYesNo(fmt.Sprintf("  Use existing %s from environment?", envName), true)
		if err != nil {
			return err
		}
		if useEnv {
			apiKey = strings.TrimSpace(os.Getenv(envName))
		}
	}
	if apiKey == "" {
		apiKey, err = promptSecret("  API key (input hidden)")
		if err != nil {
			return err
		}
	}
	if apiKey == "" {
		return errors.New("API key cannot be empty")
	}
	var addedName string
	err = storage.UpdateAccounts(accountsFile, func(store *storage.AccountStore) error {
		accountName := strings.TrimSpace(name)
		if accountName == "" {
			accountName = defaultName
		}
		for _, existing := range store.Accounts {
			if strings.EqualFold(strings.TrimSpace(existing.Name), accountName) {
				return fmt.Errorf("account name %q already exists", accountName)
			}
		}
		def, _ := providers.Get(providerID)
		store.Accounts = append(store.Accounts, storage.Account{
			ID:       storage.NewAccountID(),
			Name:     accountName,
			Provider: providerID,
			PlanType: storage.OptionalString(def.Plan),
			AuthData: storage.AuthData{Type: string(providers.APIKey), APIKey: new(apiKey)},
		})
		addedName = accountName
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Printf("  Added %s account %q.\n", defaultName, addedName)
	return nil
}

func runServerSetup(configPath string, args []string) int {
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "usage: check-usage server setup")
		return 2
	}
	if !term.IsTerminal(os.Stdin.Fd()) {
		fmt.Fprintln(os.Stderr, "error: server setup is interactive and requires a terminal")
		return 2
	}

	fmt.Println("check-usage homelab server setup")
	fmt.Println()

	config, created, err := remoteserver.EnsureInitialized(configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if created {
		fmt.Println("Initialized server config:", configPath)
	} else {
		fmt.Println("Using existing server config:", configPath)
	}

	// Step 1: Auto-detect locally installed native CLIs (agy, codex).
	store, err := storage.LoadAccountsOrEmpty(config.AccountsFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	detected := []struct {
		driver      string
		provider    string
		defaultName string
	}{
		{driver: "agy", provider: providers.Antigravity, defaultName: "Google"},
		{driver: "codex", provider: providers.Codex, defaultName: "OpenAI"},
	}
	for _, item := range detected {
		if _, err := exec.LookPath(item.driver); err != nil {
			continue
		}
		if hasNativeProviderAccount(store.Accounts, item.provider, item.driver) {
			fmt.Printf("Native %s CLI already registered.\n", item.driver)
			continue
		}
		yes, err := promptYesNo(fmt.Sprintf("Detected signed-in %q CLI on PATH. Register it?", item.driver), true)
		if err != nil {
			return 0
		}
		if yes {
			if err := registerNativeInteractive(config, item.provider, item.driver, item.defaultName); err != nil {
				fmt.Fprintln(os.Stderr, "  error:", err)
			}
		}
	}

	// Step 2: Optionally add more provider accounts.
	fmt.Println()
	for {
		fmt.Println("Add a provider account? (Enter to continue)")
		fmt.Println("  1) Claude         (browser code)")
		fmt.Println("  2) Cursor         (browser login)")
		fmt.Println("  3) OpenCode Go    (API key)")
		fmt.Println("  4) DeepSeek       (API key)")
		fmt.Println("  5) Codex          (native CLI or OAuth)")
		fmt.Println("  6) Antigravity    (native agy CLI)")
		fmt.Println("  0) Done")
		choice, err := promptLine("Choose", "0")
		if err != nil {
			return 0
		}
		switch strings.ToLower(strings.TrimSpace(choice)) {
		case "", "0", "done", "n", "no":
			goto configureNetwork
		case "1", "claude":
			name, err := promptLine("  Display name (optional)", "")
			if err != nil {
				return 0
			}
			loginArgs := []string{"--accounts-file", config.AccountsFile, "--provider", "claude"}
			if name != "" {
				loginArgs = append(loginArgs, "--name", name)
			}
			_ = runAccountsLogin(loginArgs)
		case "2", "cursor":
			name, err := promptLine("  Display name (optional)", "")
			if err != nil {
				return 0
			}
			loginArgs := []string{"--accounts-file", config.AccountsFile, "--provider", "cursor"}
			if name != "" {
				loginArgs = append(loginArgs, "--name", name)
			}
			_ = runAccountsLogin(loginArgs)
		case "3", "opencode-go", "opencode":
			if err := addAPIKeyInteractive(config.AccountsFile, providers.OpenCodeGo, "OpenCode Go"); err != nil {
				fmt.Fprintln(os.Stderr, "  error:", err)
			}
		case "4", "deepseek":
			if err := addAPIKeyInteractive(config.AccountsFile, providers.DeepSeek, "DeepSeek"); err != nil {
				fmt.Fprintln(os.Stderr, "  error:", err)
			}
		case "5", "codex":
			useNative, err := promptYesNo("  Use local native codex CLI? (No = OAuth login)", true)
			if err != nil {
				return 0
			}
			if useNative {
				if err := registerNativeInteractive(config, providers.Codex, "codex", "OpenAI"); err != nil {
					fmt.Fprintln(os.Stderr, "  error:", err)
				}
			} else {
				name, err := promptLine("  Display name (optional)", "")
				if err != nil {
					return 0
				}
				loginArgs := []string{"--accounts-file", config.AccountsFile, "--provider", "codex"}
				if name != "" {
					loginArgs = append(loginArgs, "--name", name)
				}
				_ = runAccountsLogin(loginArgs)
			}
		case "6", "antigravity", "agy":
			if err := registerNativeInteractive(config, providers.Antigravity, "agy", "Google"); err != nil {
				fmt.Fprintln(os.Stderr, "  error:", err)
			}
		default:
			fmt.Println("  Unknown choice; enter 1-6 or 0 to continue.")
		}
		fmt.Println()
	}

configureNetwork:
	// Step 3: Tailscale Serve HTTPS exposure.
	fmt.Println()
	publicURL := detectTailscaleServeURL(config.Listen)
	if publicURL != "" {
		fmt.Println("Tailscale Serve is already exposing this server at:", publicURL)
		if config.PublicURL != publicURL {
			config.PublicURL = publicURL
			_ = remoteserver.SaveConfig(configPath, config)
		}
	} else if tailscaleAvailable() {
		yes, err := promptYesNo(fmt.Sprintf("Expose server over Tailscale HTTPS (tailscale serve --bg http://%s)?", config.Listen), true)
		if err != nil {
			return 0
		}
		if yes {
			origin, err := enableTailscaleServe(config.Listen)
			if err != nil {
				fmt.Fprintln(os.Stderr, "  warning:", err)
				fmt.Fprintf(os.Stderr, "  You can run manually later: tailscale serve --bg http://%s\n", config.Listen)
			} else {
				publicURL = origin
				if publicURL != "" {
					config.PublicURL = publicURL
					_ = remoteserver.SaveConfig(configPath, config)
					fmt.Println("  Exposed at:", publicURL)
				} else {
					fmt.Println("  Tailscale Serve configured.")
				}
			}
		}
	} else {
		fmt.Printf("Tailscale CLI not found. Expose http://%s over your tailnet or pass --url when pairing.\n", config.Listen)
	}

	// Step 4: Background systemd user service (Linux).
	serviceStarted := false
	if systemdAvailable() {
		fmt.Println()
		yes, err := promptYesNo("Install and start systemd user service so the server stays running?", true)
		if err != nil {
			return 0
		}
		if yes {
			unitPath, err := installSystemdService(configPath)
			if err != nil {
				fmt.Fprintln(os.Stderr, "  warning:", err)
			} else {
				serviceStarted = true
				fmt.Println("  Installed and started:", unitPath)
				fmt.Println("  Logs: journalctl --user -u check-usage.service -f")
			}
		}
	}

	// Step 5: Pair a device right now.
	fmt.Println()
	pairNow, err := promptYesNo("Generate a pairing QR code / link for a device now?", true)
	if err == nil && pairNow {
		name, err := promptLine("  Device name (e.g. phone, laptop)", "phone")
		if err == nil && name != "" {
			allowWrite, err := promptYesNo("  Allow this device to rename/remove shared accounts?", false)
			if err == nil {
				pairing, err := remoteserver.CreatePairing(config, name, allowWrite, time.Now())
				if err != nil {
					fmt.Fprintln(os.Stderr, "  error:", err)
				} else {
					url := detectServerPublicURL(config)
					printPairingOutput(url, pairing.Code, true)
				}
			}
		}
	}

	fmt.Println()
	fmt.Println("Setup complete!")
	if !serviceStarted {
		fmt.Println("  Start the server:    check-usage server serve")
	}
	fmt.Println("  Pair more devices:   check-usage server pair <device-name>")
	fmt.Println("  Add more accounts:   check-usage server setup")
	return 0
}

func printPairingOutput(serverURL, code string, showQR bool) {
	fmt.Println()
	if serverURL != "" {
		if invite, err := remote.FormatInvite(serverURL, code); err == nil {
			if showQR {
				if encoded, err := qr.Encode(invite); err == nil {
					ansi := os.Getenv("NO_COLOR") == ""
					fmt.Println(encoded.RenderTerminal(ansi))
					fmt.Println()
				}
			}
			fmt.Println("Pairing link (scan QR on Android, or paste into `check-usage connect` / Android Server dialog):")
			fmt.Println(" ", invite)
			fmt.Println()
			fmt.Println("Or enter manually:")
			fmt.Println("  Server URL:  ", serverURL)
			fmt.Println("  Pairing code:", code)
			fmt.Fprintln(os.Stderr, "\nExpires in 10 minutes and can be used once.")
			return
		}
	}
	fmt.Println(code)
	fmt.Fprintln(os.Stderr, "Pairing expires in ten minutes and can be used once.")
	fmt.Fprintln(os.Stderr, "Tip: run `tailscale serve --bg http://127.0.0.1:8787` or pass `--url https://...` to include a QR code and one-paste pairing link.")
}

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
	"time"

	"github.com/McMelonTV/check-usage/internal/nativecli"
	"github.com/McMelonTV/check-usage/internal/remoteserver"
	"github.com/McMelonTV/check-usage/usageapi"
)

func runServerCommand(args []string) int {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	configPath := fs.String("config", remoteserver.DefaultConfigPath(), "server configuration file")
	if fs.Parse(args) != nil {
		return 2
	}
	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: check-usage server [--config path] init|serve|pair|devices|accounts|api")
		return 2
	}
	command, rest := fs.Arg(0), fs.Args()[1:]
	if command == "init" {
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
	}
	config, err := remoteserver.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	switch command {
	case "serve":
		if len(rest) != 0 {
			return 2
		}
		service := usageapi.New(usageapi.Config{AccountsFile: config.AccountsFile, CacheDir: config.CacheDir, MinRefreshInterval: time.Minute})
		handler := &remoteserver.Handler{Config: config, Service: service}
		server := &http.Server{Addr: config.Listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 3 * time.Minute, IdleTimeout: time.Minute, MaxHeaderBytes: 16 << 10}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		server.BaseContext = func(net.Listener) context.Context { return ctx }
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
		pairFlags := flag.NewFlagSet("server pair", flag.ContinueOnError)
		name := pairFlags.String("name", "", "device name")
		write := pairFlags.Bool("allow-account-changes", false, "allow this device to rename/remove shared accounts")
		out := pairFlags.String("out", "", "write one-time pairing code to a private file")
		if pairFlags.Parse(rest) != nil || pairFlags.NArg() != 0 {
			return 2
		}
		pairing, err := remoteserver.CreatePairing(config, *name, *write, time.Now())
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		if *out != "" {
			file, err := os.OpenFile(*out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
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
		} else {
			fmt.Println(pairing.Code)
		}
		fmt.Fprintln(os.Stderr, "Pairing expires in ten minutes and can be used once.")
		return 0
	case "devices":
		if len(rest) == 2 && rest[0] == "revoke" {
			if err := remoteserver.RevokeDevice(config, rest[1]); err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				return 1
			}
			fmt.Println("Device revoked.")
			return 0
		}
		if len(rest) > 1 || len(rest) == 1 && rest[0] != "list" {
			return 2
		}
		devices, err := remoteserver.ListDevices(config)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		_ = json.NewEncoder(os.Stdout).Encode(devices)
		return 0
	case "api":
		return runAPICommand(append([]string{"--accounts-file", config.AccountsFile, "--cache-dir", config.CacheDir}, rest...))
	case "accounts":
		if len(rest) == 0 {
			return 2
		}
		if rest[0] == "native" {
			return runNativeAccount(config, rest[1:])
		}
		// Existing provider sign-in and API-key administration now run only here.
		localArgs := append([]string{rest[0], "--accounts-file", config.AccountsFile}, rest[1:]...)
		return runAccountsCommand(localArgs)
	default:
		fmt.Fprintln(os.Stderr, "unknown server command:", command)
		return 2
	}
}

func runNativeAccount(server remoteserver.Config, args []string) int {
	fs := flag.NewFlagSet("server accounts native", flag.ContinueOnError)
	provider := fs.String("provider", "", "provider ID")
	name := fs.String("name", "", "account display name")
	account := fs.String("account", "", "attach to an existing server account")
	driver := fs.String("driver", "", "native driver: agy, codex, or json")
	executable := fs.String("executable", "", "native executable or adapter path")
	source := fs.String("source-file", "", "native adapter configuration JSON file")
	configDir := fs.String("config-dir", "", "Codex configuration directory for this profile")
	fallback := fs.Bool("fallback", false, "allow the existing account's API credentials as fallback")
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
	_ = json.NewEncoder(os.Stdout).Encode(result)
	return 0
}

package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/McMelonTV/check-usage/internal/remote"
	"github.com/McMelonTV/check-usage/internal/remoteserver"
	"github.com/McMelonTV/check-usage/internal/storage"
	"github.com/McMelonTV/check-usage/usageapi"
	"github.com/charmbracelet/x/term"
)

func runConnectCommand(args []string) int {
	fs := flag.NewFlagSet("connect", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	setDoubleDashFlagUsage(fs)
	path := fs.String("connection-file", remote.DefaultConfigPath(), "device connection file")
	address := fs.String("url", "", "server HTTPS origin (optional when pasting a pairing link)")
	codeFile := fs.String("pair-code-file", "-", "one-time pairing code or invite link file, or - for stdin")
	if fs.Parse(args) != nil || fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: check-usage connect [--url https://server.tailnet.ts.net] [--pair-code-file path|-]")
		return 2
	}
	var reader io.Reader = os.Stdin
	if *codeFile != "-" {
		file, err := os.Open(*codeFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error: cannot read pairing code file")
			return 1
		}
		defer file.Close()
		reader = file
	}
	config, err := connectFromReader(*path, strings.TrimSpace(*address), reader, *codeFile == "-" && term.IsTerminal(os.Stdin.Fd()))
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	fmt.Println("Connected to", config.URL)
	return 0
}

func connectFromReader(connectionPath, serverURL string, reader io.Reader, interactive bool) (remote.Config, error) {
	buf := bufio.NewReader(io.LimitReader(reader, 4096))
	readLine := func() (string, error) {
		line, err := buf.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", err
		}
		return strings.TrimSpace(line), nil
	}

	var code string
	if serverURL == "" {
		if interactive {
			fmt.Fprint(os.Stderr, "Paste pairing link (or server URL): ")
		}
		first, err := readLine()
		if err != nil {
			return remote.Config{}, err
		}
		if first == "" {
			return remote.Config{}, fmt.Errorf("server URL or pairing link is required")
		}
		if inviteURL, inviteCode, ok, err := remote.ParseInvite(first); ok {
			if err != nil {
				return remote.Config{}, err
			}
			serverURL, code = inviteURL, inviteCode
		} else {
			serverURL = first
		}
	}

	if code == "" {
		if interactive {
			fmt.Fprint(os.Stderr, "Enter one-time pairing code: ")
		}
		entered, err := readLine()
		if err != nil {
			return remote.Config{}, err
		}
		if inviteURL, inviteCode, ok, err := remote.ParseInvite(entered); ok {
			if err != nil {
				return remote.Config{}, err
			}
			if serverURL == "" {
				serverURL = inviteURL
			}
			code = inviteCode
		} else {
			code = entered
		}
	}

	config, err := remote.Pair(context.Background(), serverURL, code, nil)
	if err != nil {
		return remote.Config{}, err
	}
	client, err := remote.New(config, nil)
	if err != nil {
		return remote.Config{}, err
	}
	if _, err = client.Info(context.Background()); err != nil {
		return remote.Config{}, err
	}
	if err := remote.SaveConfig(connectionPath, config); err != nil {
		return remote.Config{}, err
	}
	return config, nil
}
func loadClientConfig(connectionPath string) (remote.Config, error) {
	defaultServerPath := ""
	if filepath.Clean(connectionPath) == filepath.Clean(remote.DefaultConfigPath()) {
		defaultServerPath = remoteserver.DefaultConfigPath()
	}
	return loadOrAutoConnectClientConfig(connectionPath, defaultServerPath)
}

func loadOrAutoConnectClientConfig(connectionPath, serverConfigPath string) (remote.Config, error) {
	config, err := remote.LoadConfig(connectionPath)
	if err == nil {
		return config, nil
	}
	if serverConfigPath == "" {
		return remote.Config{}, err
	}
	if _, statErr := os.Stat(connectionPath); !os.IsNotExist(statErr) {
		return remote.Config{}, err
	}
	serverCfg, serverErr := remoteserver.LoadConfig(serverConfigPath)
	if serverErr != nil {
		return remote.Config{}, err
	}
	return ensureLocalServerClient(serverCfg, connectionPath)
}

func ensureLocalServerClient(serverCfg remoteserver.Config, connectionPath string) (remote.Config, error) {
	if existing, err := remote.LoadConfig(connectionPath); err == nil {
		return existing, nil
	}
	if _, statErr := os.Stat(connectionPath); !os.IsNotExist(statErr) {
		return remote.Config{}, fmt.Errorf("existing client config at %s is unreadable", connectionPath)
	}
	localURL, err := remoteserver.LocalLoopbackURL(serverCfg.Listen)
	if err != nil {
		return remote.Config{}, err
	}
	deviceName := "local"
	if host, err := os.Hostname(); err == nil && strings.TrimSpace(host) != "" {
		deviceName = "local (" + strings.TrimSpace(host) + ")"
	}
	paired, err := remoteserver.ProvisionLocalDevice(serverCfg, deviceName, true, time.Now())
	if err != nil {
		return remote.Config{}, err
	}
	cfg := remote.Config{
		URL:      localURL,
		Token:    paired.Token,
		ServerID: paired.ServerID,
	}
	if err := remote.SaveConfig(connectionPath, cfg); err != nil {
		return remote.Config{}, err
	}
	return cfg, nil
}

func connectedClient(path string, timeout time.Duration) (*remote.Client, error) {
	config, err := loadClientConfig(path)
	if err != nil {
		return nil, err
	}
	client, err := remote.New(config, &http.Client{Timeout: timeout})
	if err != nil {
		return nil, err
	}
	_, err = client.Info(context.Background())
	return client, err
}

func runRemoteAccountsCommand(args []string) int {
	fs := flag.NewFlagSet("accounts", flag.ContinueOnError)
	path := fs.String("connection-file", remote.DefaultConfigPath(), "device connection file")
	if fs.Parse(args) != nil || fs.NArg() == 0 {
		return 2
	}
	client, err := connectedClient(*path, 2*time.Minute)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	switch fs.Arg(0) {
	case "list":
		accounts, err := client.Accounts(context.Background())
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		local := make([]storage.Account, 0, len(accounts))
		for _, account := range accounts {
			local = append(local, remote.LocalAccount(account))
		}
		printAccountsList(local)
		return 0
	case "rename", "remove":
		params := map[string]string{}
		if fs.Arg(0) == "rename" && fs.NArg() == 3 {
			params["account"], params["new_name"] = fs.Arg(1), fs.Arg(2)
		} else if fs.Arg(0) == "remove" && fs.NArg() == 2 {
			params["account"] = fs.Arg(1)
		} else {
			return 2
		}
		var result usageapi.AccountMutation
		if err := client.Call(context.Background(), "accounts."+fs.Arg(0), params, &result); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		fmt.Println(result.Action, result.Account.Name)
		return 0
	default:
		fmt.Fprintln(os.Stderr, "Manage provider logins on the server with check-usage server accounts; this client synchronizes its account list.")
		return 2
	}
}
func runRemoteAPICommand(args []string) int {
	fs := flag.NewFlagSet("api", flag.ContinueOnError)
	path := fs.String("connection-file", remote.DefaultConfigPath(), "device connection file")
	if fs.Parse(args) != nil || fs.NArg() < 1 || fs.NArg() > 2 {
		return 2
	}
	client, err := connectedClient(*path, 2*time.Minute)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	params := json.RawMessage("{}")
	if fs.NArg() == 2 {
		value := fs.Arg(1)
		if value == "-" {
			data, err := io.ReadAll(io.LimitReader(os.Stdin, (1<<20)+1))
			if err != nil {
				return 1
			}
			value = string(data)
		}
		if !json.Valid([]byte(value)) {
			return 2
		}
		params = json.RawMessage(value)
	}
	var result json.RawMessage
	if err := client.Call(context.Background(), fs.Arg(0), params, &result); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	_ = json.NewEncoder(os.Stdout).Encode(usageapi.RPCResponse{JSONRPC: "2.0", ID: json.RawMessage("1"), Result: result})
	return 0
}
func runRemoteResetsCommand(args []string) int {
	fs := flag.NewFlagSet("resets", flag.ContinueOnError)
	path := fs.String("connection-file", remote.DefaultConfigPath(), "device connection file")
	if fs.Parse(args) != nil || fs.NArg() != 1 {
		return 2
	}
	client, err := connectedClient(*path, 2*time.Minute)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	result, err := client.Resets(context.Background(), fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	printResetCreditsDetails(remote.LocalAccount(result.Account), remote.LocalCredits(result.Credits), time.Now())
	return 0
}

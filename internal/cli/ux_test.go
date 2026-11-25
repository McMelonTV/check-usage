package cli

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/McMelonTV/check-usage/internal/nativecli"
	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/internal/remote"
	"github.com/McMelonTV/check-usage/internal/remoteserver"
	"github.com/McMelonTV/check-usage/internal/storage"
	"github.com/McMelonTV/check-usage/usageapi"
)

func TestConnectWithPairingInviteLinkWithoutURLFlag(t *testing.T) {
	dir := t.TempDir()
	config := remoteserver.Config{
		ServerID:     "invite-server",
		AccountsFile: filepath.Join(dir, "accounts.json"),
		CacheDir:     filepath.Join(dir, "cache"),
		StateDir:     filepath.Join(dir, "state"),
	}
	service := usageapi.New(usageapi.Config{AccountsFile: config.AccountsFile, CacheDir: config.CacheDir})
	server := httptest.NewServer(&remoteserver.Handler{Config: config, Service: service})
	defer server.Close()

	pairing, err := remoteserver.CreatePairing(config, "laptop", false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	invite, err := remote.FormatInvite(server.URL, pairing.Code)
	if err != nil {
		t.Fatal(err)
	}

	inviteFile := filepath.Join(dir, "invite.txt")
	if err := os.WriteFile(inviteFile, []byte(invite+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	connectionFile := filepath.Join(dir, "client.json")
	if code := Run([]string{"check-usage", "connect", "--pair-code-file", inviteFile, "--connection-file", connectionFile}); code != 0 {
		t.Fatalf("connect with invite link exited with %d", code)
	}
	saved, err := remote.LoadConfig(connectionFile)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if saved.URL != server.URL || saved.ServerID != config.ServerID {
		t.Fatalf("unexpected saved connection: %#v", saved)
	}
}

func TestServerPairAutoInitPositionalNameAndRevokeByName(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "server.json")
	outFile := filepath.Join(dir, "code.txt")

	// Auto-initializes server.json and accepts positional name before flags.
	if code := Run([]string{"check-usage", "server", "--config", configPath, "pair", "my-laptop", "--allow-account-changes", "--out", outFile}); code != 0 {
		t.Fatalf("server pair exited with %d", code)
	}
	cfg, err := remoteserver.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("expected server.json to be auto-initialized: %v", err)
	}
	codeBytes, err := os.ReadFile(outFile)
	if err != nil || len(strings.TrimSpace(string(codeBytes))) != 43 {
		t.Fatalf("unexpected pairing code file content: %q err=%v", string(codeBytes), err)
	}

	// Redeem pairing so device exists, then revoke by name.
	service := usageapi.New(usageapi.Config{AccountsFile: cfg.AccountsFile, CacheDir: cfg.CacheDir})
	server := httptest.NewServer(&remoteserver.Handler{Config: cfg, Service: service})
	defer server.Close()
	if _, err := remote.Pair(context.Background(), server.URL, strings.TrimSpace(string(codeBytes)), server.Client()); err != nil {
		t.Fatalf("Pair() error = %v", err)
	}
	if code := Run([]string{"check-usage", "server", "--config", configPath, "devices", "revoke", "my-laptop"}); code != 0 {
		t.Fatalf("revoke by name exited with %d", code)
	}
	devices, err := remoteserver.ListDevices(cfg)
	if err != nil || len(devices) != 0 {
		t.Fatalf("expected device to be revoked, remaining: %#v err=%v", devices, err)
	}
}

func TestTailscaleJSONParsing(t *testing.T) {
	serveJSON := []byte(`{
		"Web": {
			"homelab.tail1234.ts.net:443": {
				"Handlers": {
					"/": {"Proxy": "http://127.0.0.1:8787"}
				}
			}
		}
	}`)
	if got := parseTailscaleServeJSON(serveJSON, "127.0.0.1:8787"); got != "https://homelab.tail1234.ts.net" {
		t.Fatalf("parseTailscaleServeJSON() = %q, want https://homelab.tail1234.ts.net", got)
	}
	if got := parseTailscaleServeJSON(serveJSON, "127.0.0.1:9999"); got != "" {
		t.Fatalf("expected non-matching port to return empty, got %q", got)
	}

	statusJSON := []byte(`{"Self":{"DNSName":"homelab.tail1234.ts.net."}}`)
	if got := parseTailscaleStatusDNS(statusJSON); got != "https://homelab.tail1234.ts.net" {
		t.Fatalf("parseTailscaleStatusDNS() = %q", got)
	}
}

func TestConcurrentAccountMutationsWithFileLock(t *testing.T) {
	dir := t.TempDir()
	accountsPath := filepath.Join(dir, "accounts.json")
	service := usageapi.New(usageapi.Config{AccountsFile: accountsPath, CacheDir: filepath.Join(dir, "cache")})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := range 5 {
			_, err := service.SaveNativeAccount(usageapi.NativeAccount{
				Provider: providers.Codex,
				Name:     "Codex " + string(rune('A'+i)),
				Config:   nativecli.Config{Driver: "codex"},
			})
			if err != nil {
				t.Errorf("SaveNativeAccount() error = %v", err)
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := range 5 {
			name := "DeepSeek " + string(rune('A'+i))
			err := storage.UpdateAccounts(accountsPath, func(store *storage.AccountStore) error {
				store.Accounts = append(store.Accounts, storage.Account{
					ID:       storage.NewAccountID(),
					Name:     name,
					Provider: providers.DeepSeek,
					AuthData: storage.AuthData{Type: string(providers.APIKey), APIKey: new("secret")},
				})
				return nil
			})
			if err != nil {
				t.Errorf("UpdateAccounts() error = %v", err)
			}
		}
	}()
	wg.Wait()

	accounts, err := service.ListAccounts()
	if err != nil {
		t.Fatalf("ListAccounts() error = %v", err)
	}
	if len(accounts) != 10 {
		t.Fatalf("len(accounts) = %d, want 10 (lost concurrent update!)", len(accounts))
	}
}

func TestSystemdUnitGenerationOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only")
	}
	unit := systemdUnitContent("/home/me/.local/bin/check-usage", "/srv/check-usage/server.json", "/home/me/.local/bin:/usr/bin")
	for _, want := range []string{
		`ExecStart="/home/me/.local/bin/check-usage" server --config "/srv/check-usage/server.json" serve`,
		`Environment="PATH=/home/me/.local/bin:/usr/bin"`,
		`Restart=on-failure`,
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("unit missing %q:\n%s", want, unit)
		}
	}
}

func TestLocalServerAutoConnectsClientConfig(t *testing.T) {
	dir := t.TempDir()
	serverPath := filepath.Join(dir, "server.json")
	clientPath := filepath.Join(dir, "client.json")

	serverCfg := remoteserver.Config{
		ServerID:     "local-server-id",
		Listen:       "127.0.0.1:8787",
		AccountsFile: filepath.Join(dir, "accounts.json"),
		CacheDir:     filepath.Join(dir, "cache"),
		StateDir:     filepath.Join(dir, "state"),
	}
	if err := remoteserver.SaveConfig(serverPath, serverCfg); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadOrAutoConnectClientConfig(clientPath, serverPath)
	if err != nil {
		t.Fatalf("loadOrAutoConnectClientConfig() error = %v", err)
	}
	if cfg.URL != "http://127.0.0.1:8787" || cfg.ServerID != "local-server-id" || cfg.Token == "" {
		t.Fatalf("unexpected auto-connected config: %#v", cfg)
	}

	// Second load should reuse the saved client.json without creating another device.
	cfg2, err := loadOrAutoConnectClientConfig(clientPath, serverPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.Token != cfg.Token {
		t.Fatalf("expected existing client.json token to be reused")
	}
	devices, err := remoteserver.ListDevices(serverCfg)
	if err != nil || len(devices) != 1 || devices[0].Access != "write" {
		t.Fatalf("expected exactly one local write device, got %#v err=%v", devices, err)
	}
}

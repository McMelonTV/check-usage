package cli

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/McMelonTV/check-usage/internal/nativecli"
	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/internal/remote"
	"github.com/McMelonTV/check-usage/internal/remoteserver"
	"github.com/McMelonTV/check-usage/internal/tui"
	"github.com/McMelonTV/check-usage/usageapi"
)

func TestCLIConnectAndCommandsUseSharedServer(t *testing.T) {
	dir := t.TempDir()
	config := remoteserver.Config{ServerID: "fixture", AccountsFile: filepath.Join(dir, "server-accounts.json"), CacheDir: filepath.Join(dir, "server-cache"), StateDir: filepath.Join(dir, "state")}
	service := usageapi.New(usageapi.Config{AccountsFile: config.AccountsFile, CacheDir: config.CacheDir, NativeFetch: func(context.Context, string, nativecli.Config) (nativecli.Result, error) {
		return nativecli.Result{Usage: providers.Usage{Metrics: []providers.Metric{{Kind: providers.Percentage, Slot: providers.SessionSlot, Label: "SESSION", Used: new(float64(25))}}}}, nil
	}})
	account, err := service.SaveNativeAccount(usageapi.NativeAccount{Provider: providers.Codex, Name: "Server Codex", Config: nativecli.Config{Driver: "codex"}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(&remoteserver.Handler{Config: config, Service: service})
	defer server.Close()
	pairing, err := remoteserver.CreatePairing(config, "CLI", true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	codeFile, connectionFile := filepath.Join(dir, "pair-code"), filepath.Join(dir, "client.json")
	if err := os.WriteFile(codeFile, []byte(pairing.Code+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if code := Run([]string{"check-usage", "connect", "--url", server.URL, "--pair-code-file", codeFile, "--connection-file", connectionFile}); code != 0 {
		t.Fatalf("connect exit %d", code)
	}
	if info, err := os.Stat(connectionFile); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("device token storage permissions are unsafe")
	}
	if code := Run([]string{"check-usage", "--plain", "--connection-file", connectionFile}); code != 0 {
		t.Fatal("plain remote client failed")
	}
	if code := Run([]string{"check-usage", "accounts", "--connection-file", connectionFile, "rename", account.Account.ID, "Shared name"}); code != 0 {
		t.Fatal("remote rename failed")
	}
	accounts, err := service.ListAccounts()
	if err != nil || accounts[0].Name != "Shared name" {
		t.Fatal("CLI changed a local file instead of server metadata")
	}
	if code := Run([]string{"check-usage", "api", "--connection-file", connectionFile, "accounts.list"}); code != 0 {
		t.Fatal("remote RPC command failed")
	}
	connection, err := remote.LoadConfig(connectionFile)
	if err != nil {
		t.Fatal(err)
	}
	client, err := remote.New(connection, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	model := tui.NewRemoteModel(connectionFile, client)
	if model.Init() == nil {
		t.Fatal("remote dashboard did not initialize")
	}
}

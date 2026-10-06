package tui

import (
	"path/filepath"
	"testing"

	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/internal/storage"
)

func TestTUIAccountRenameAndRemoveCommands(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "accounts.json")
	store := &storage.AccountStore{
		Accounts: []storage.Account{{ID: "one", Name: "Old name", Provider: providers.Codex, AuthData: storage.AuthData{Type: "chatgpt"}}},
	}
	if err := storage.SaveAccounts(path, store); err != nil {
		t.Fatalf("saveAccounts() error = %v", err)
	}
	if err := storage.SaveAccountCache("one", storage.CacheEntry{FetchedAt: 123}); err != nil {
		t.Fatalf("saveAccountUsageCache() error = %v", err)
	}

	renameMsg := renameAccountCmd(path, "one", "New name")().(storeSavedMsg)
	if renameMsg.err != nil || !renameMsg.reload {
		t.Fatalf("rename command = %#v", renameMsg)
	}
	loaded, err := storage.LoadAccounts(path)
	if err != nil || loaded.Accounts[0].Name != "New name" {
		t.Fatalf("renamed account = %#v, error = %v", loaded, err)
	}

	removeMsg := removeAccountCmd(path, "one", "New name")().(storeSavedMsg)
	if removeMsg.err != nil || !removeMsg.reload {
		t.Fatalf("remove command = %#v", removeMsg)
	}
	loaded, err = storage.LoadAccounts(path)
	if err != nil || len(loaded.Accounts) != 0 {
		t.Fatalf("accounts after removal = %#v, error = %v", loaded, err)
	}
	if _, ok, err := storage.LoadAccountCache("one"); err != nil || ok {
		t.Fatalf("account cache still exists: ok = %v, error = %v", ok, err)
	}
}

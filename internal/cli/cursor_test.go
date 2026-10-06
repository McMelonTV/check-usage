package cli

import (
	"path/filepath"
	"testing"

	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/internal/storage"
)

func TestCursorAccountCLIAddAndReauthenticate(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("TEST_CURSOR_KEY", "key-secret")
	path := filepath.Join(t.TempDir(), "accounts.json")
	if code := runAccountsAdd([]string{"--accounts-file", path, "--provider", "cursor", "--api-key-env", "TEST_CURSOR_KEY"}); code != 0 {
		t.Fatalf("add exited with %d", code)
	}
	store, err := storage.LoadAccounts(path)
	if err != nil {
		t.Fatal(err)
	}
	account := store.Accounts[0]
	if account.Provider != providers.Cursor || account.AuthData.Type != "api_key" || storage.StringValue(account.AuthData.APIKey) != "key-secret" {
		t.Fatal("Cursor credential was stored in the wrong format")
	}
	if err := storage.SaveAccountCache(account.ID, storage.CacheEntry{FetchedAt: 1}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_CURSOR_KEY", "replacement-secret")
	if code := runAccountsReauth([]string{"--accounts-file", path, "--api-key-env", "TEST_CURSOR_KEY", account.ID}); code != 0 {
		t.Fatalf("reauth exited with %d", code)
	}
	store, err = storage.LoadAccounts(path)
	if err != nil || storage.StringValue(store.Accounts[0].AuthData.APIKey) != "replacement-secret" {
		t.Fatal("session was not replaced")
	}
	if _, cached, err := storage.LoadAccountCache(account.ID); err != nil || cached {
		t.Fatal("reauth did not invalidate cached usage")
	}
	for _, flags := range [][]string{{}, {"--api-key", ""}, {"--api-key", "key-secret", "--api-key-env", "TEST_CURSOR_KEY"}} {
		args := append([]string{"--accounts-file", path, "--provider", "cursor"}, flags...)
		if code := runAccountsAdd(args); code != 2 {
			t.Fatalf("invalid credential flags exited with %d", code)
		}
	}
}

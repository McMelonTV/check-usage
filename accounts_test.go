package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSettingsUseSeparateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.json")
	settings := appSettings{UsageDisplay: "remaining", BarFill: "right", PercentagePosition: "left", ColorTheme: "colorblind", AutoRefreshSeconds: 0, CompactMode: true}
	if err := saveSettings(path, settings); err != nil {
		t.Fatalf("saveSettings() error = %v", err)
	}
	loaded, err := loadSettings(path)
	if err != nil {
		t.Fatalf("loadSettings() error = %v", err)
	}
	if loaded.UsageDisplay != "remaining" || loaded.BarFill != "right" || loaded.PercentagePosition != "left" || loaded.ColorTheme != "colorblind" || loaded.AutoRefreshSeconds != 0 || !loaded.CompactMode {
		t.Fatalf("settings did not round-trip: %#v", loaded)
	}
	if settingsPath(path) == path || filepath.Base(settingsPath(path)) != "settings.json" {
		t.Fatalf("settings path = %q", settingsPath(path))
	}
}

func TestAccountsFileDoesNotContainSettingsOrCache(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "accounts.json")
	store, err := parseAccounts([]byte(`{"accounts":[],"settings":{"usage_display":"remaining"},"usage_cache":{"one":{"fetched_at":123}}}`))
	if err != nil {
		t.Fatalf("parseAccounts() error = %v", err)
	}
	if err := saveAccounts(path, store); err != nil {
		t.Fatalf("saveAccounts() error = %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.Contains(string(content), "settings") || strings.Contains(string(content), "usage_cache") {
		t.Fatalf("accounts file contains unrelated persistence data: %s", content)
	}
	settings, err := loadSettings(path)
	if err != nil || settings.UsageDisplay != "used" {
		t.Fatalf("embedded settings were read: %#v, error = %v", settings, err)
	}
	if _, ok, err := loadAccountUsageCache("one"); err != nil || ok {
		t.Fatalf("embedded cache was read: ok = %v, error = %v", ok, err)
	}
}

func TestTUIAccountRenameAndRemoveCommands(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "accounts.json")
	store := &accountsStore{
		Accounts: []storedAccount{{ID: "one", Name: "Old name", Provider: providerOpenAICodex, AuthData: authData{Type: "chatgpt"}}},
	}
	if err := saveAccounts(path, store); err != nil {
		t.Fatalf("saveAccounts() error = %v", err)
	}
	if err := saveAccountUsageCache("one", usageCacheEntry{FetchedAt: 123}); err != nil {
		t.Fatalf("saveAccountUsageCache() error = %v", err)
	}

	renameMsg := renameAccountCmd(path, "one", "New name")().(storeSavedMsg)
	if renameMsg.err != nil || !renameMsg.reload {
		t.Fatalf("rename command = %#v", renameMsg)
	}
	loaded, err := loadAccounts(path)
	if err != nil || loaded.Accounts[0].Name != "New name" {
		t.Fatalf("renamed account = %#v, error = %v", loaded, err)
	}

	removeMsg := removeAccountCmd(path, "one", "New name")().(storeSavedMsg)
	if removeMsg.err != nil || !removeMsg.reload {
		t.Fatalf("remove command = %#v", removeMsg)
	}
	loaded, err = loadAccounts(path)
	if err != nil || len(loaded.Accounts) != 0 {
		t.Fatalf("accounts after removal = %#v, error = %v", loaded, err)
	}
	if _, ok, err := loadAccountUsageCache("one"); err != nil || ok {
		t.Fatalf("account cache still exists: ok = %v, error = %v", ok, err)
	}
}

package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSettingsUseSeparateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.json")
	settings := Settings{UsageDisplay: "remaining", BarFill: "right", BarOrder: "percent_bar_reset", ShowBar: new(true), ShowPercent: new(true), ShowReset: new(false), ColorTheme: "colorblind", AutoRefreshSeconds: 0, CompactMode: true}
	if err := SaveSettings(path, settings); err != nil {
		t.Fatalf("saveSettings() error = %v", err)
	}
	loaded, err := LoadSettings(path)
	if err != nil {
		t.Fatalf("loadSettings() error = %v", err)
	}
	if loaded.UsageDisplay != "remaining" || loaded.BarFill != "right" || loaded.BarOrder != "percent_bar_reset" || !loaded.BarVisible() || !loaded.PercentVisible() || loaded.ResetVisible() || loaded.ColorTheme != "colorblind" || loaded.AutoRefreshSeconds != 0 || !loaded.CompactMode {
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
	if err := SaveAccounts(path, store); err != nil {
		t.Fatalf("saveAccounts() error = %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.Contains(string(content), "settings") || strings.Contains(string(content), "usage_cache") {
		t.Fatalf("accounts file contains unrelated persistence data: %s", content)
	}
	settings, err := LoadSettings(path)
	if err != nil || settings.UsageDisplay != "used" {
		t.Fatalf("embedded settings were read: %#v, error = %v", settings, err)
	}
	if _, ok, err := LoadAccountCache("one"); err != nil || ok {
		t.Fatalf("embedded cache was read: ok = %v, error = %v", ok, err)
	}
}

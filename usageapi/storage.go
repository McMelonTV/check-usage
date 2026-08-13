package usageapi

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultAccountsPath returns the accounts file used by the CLI.
func DefaultAccountsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".config", "check-usage", "accounts.json")
	}
	return filepath.Join(home, ".config", "check-usage", "accounts.json")
}

// DefaultCacheDir returns the per-account cache directory used by the CLI.
func DefaultCacheDir() string {
	root, err := os.UserCacheDir()
	if err != nil {
		return filepath.Join(".cache", "check-usage", "accounts")
	}
	return filepath.Join(root, "check-usage", "accounts")
}

func defaultSettings() Settings {
	return Settings{
		UsageDisplay: "used", BarFill: "left", PercentagePosition: "right",
		ColorTheme: "default", AutoRefreshSeconds: 60,
	}
}

func (service *Service) loadAccounts() (*accountsStore, error) {
	content, err := os.ReadFile(service.accountsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return &accountsStore{Accounts: []storedAccount{}}, nil
		}
		return nil, err
	}
	if strings.TrimSpace(string(content)) == "" {
		return &accountsStore{Accounts: []storedAccount{}}, nil
	}
	var store accountsStore
	if err := json.Unmarshal(content, &store); err != nil {
		return nil, fmt.Errorf("decode accounts file: %w", err)
	}
	if store.Accounts == nil {
		store.Accounts = []storedAccount{}
	}
	return &store, nil
}

func (service *Service) saveAccounts(store *accountsStore) error {
	return writeJSONFile(service.accountsFile, store)
}

func (service *Service) loadSettings() (Settings, error) {
	settings := defaultSettings()
	content, err := os.ReadFile(service.settingsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return settings, nil
		}
		return Settings{}, err
	}
	if err := json.Unmarshal(content, &settings); err != nil {
		return Settings{}, fmt.Errorf("decode settings file: %w", err)
	}
	normalizeSettings(&settings)
	return settings, nil
}

func (service *Service) saveSettings(settings Settings) error {
	normalizeSettings(&settings)
	return writeJSONFile(service.settingsPath(), settings)
}

func (service *Service) settingsPath() string {
	return filepath.Join(filepath.Dir(service.accountsFile), "settings.json")
}

func (service *Service) cachePath(accountID string) string {
	name := base64.RawURLEncoding.EncodeToString([]byte(accountID)) + ".json"
	return filepath.Join(service.cacheDir, name)
}

func (service *Service) loadCache(accountID string) (cacheEntry, bool, error) {
	content, err := os.ReadFile(service.cachePath(accountID))
	if err != nil {
		if os.IsNotExist(err) {
			return cacheEntry{}, false, nil
		}
		return cacheEntry{}, false, err
	}
	var entry cacheEntry
	if err := json.Unmarshal(content, &entry); err != nil {
		return cacheEntry{}, false, fmt.Errorf("decode usage cache: %w", err)
	}
	return entry, true, nil
}

func (service *Service) saveCache(accountID string, entry cacheEntry) error {
	return writeJSONFile(service.cachePath(accountID), entry)
}

func writeJSONFile(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o600)
}

func normalizeSettings(settings *Settings) {
	if settings.UsageDisplay != "used" && settings.UsageDisplay != "remaining" {
		settings.UsageDisplay = "used"
	}
	if settings.BarFill != "left" && settings.BarFill != "right" {
		settings.BarFill = "left"
	}
	if settings.PercentagePosition != "left" && settings.PercentagePosition != "right" {
		settings.PercentagePosition = "right"
	}
	if settings.ColorTheme != "default" && settings.ColorTheme != "colorblind" && settings.ColorTheme != "monochrome" {
		settings.ColorTheme = "default"
	}
	validRefresh := false
	for _, seconds := range []int{0, 30, 60, 300, 900} {
		validRefresh = validRefresh || settings.AutoRefreshSeconds == seconds
	}
	if !validRefresh {
		settings.AutoRefreshSeconds = 60
	}
}

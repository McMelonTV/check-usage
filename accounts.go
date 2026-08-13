package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func loadAccounts(path string) (*accountsStore, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("accounts file not found: %s", path)
		}
		return nil, err
	}
	return parseAccounts(content)
}

func loadAccountsOrEmpty(path string) (*accountsStore, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return emptyAccountsStore(), nil
		}
		return nil, err
	}
	return parseAccounts(content)
}

func parseAccounts(content []byte) (*accountsStore, error) {
	if strings.TrimSpace(string(content)) == "" {
		return emptyAccountsStore(), nil
	}
	store := accountsStore{}
	if err := json.Unmarshal(content, &store); err != nil {
		return nil, err
	}
	if store.Accounts == nil {
		store.Accounts = []storedAccount{}
	}
	return &store, nil
}

func emptyAccountsStore() *accountsStore {
	return &accountsStore{Accounts: []storedAccount{}, needsSave: true}
}

func defaultAppSettings() appSettings {
	showBar, showPercent, showReset := true, true, true
	return appSettings{UsageDisplay: "used", BarFill: "left", BarOrder: "bar_percent_reset", ShowBar: &showBar, ShowPercent: &showPercent, ShowReset: &showReset, ColorTheme: "default", AutoRefreshSeconds: 60, CompactMode: false}
}

func saveAccounts(path string, store *accountsStore) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	store.needsSave = false
	return nil
}

func defaultAccountsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return defaultAccountsRelPath
	}
	return filepath.Join(home, defaultAccountsRelPath)
}

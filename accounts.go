package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
			return &accountsStore{Accounts: []storedAccount{}}, nil
		}
		return nil, err
	}
	return parseAccounts(content)
}

func parseAccounts(content []byte) (*accountsStore, error) {
	var store accountsStore
	if err := json.Unmarshal(content, &store); err != nil {
		return nil, err
	}
	return &store, nil
}

func saveAccounts(path string, store *accountsStore) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func defaultAccountsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return defaultAccountsRelPath
	}
	return filepath.Join(home, defaultAccountsRelPath)
}

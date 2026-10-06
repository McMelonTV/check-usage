// Package storage persists saved accounts, dashboard settings, and per-account usage snapshots on disk.
package storage

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/McMelonTV/check-usage/internal/providers"
)

func LoadAccounts(path string) (*AccountStore, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("accounts file not found: %s", path)
		}
		return nil, err
	}
	return parseAccounts(content)
}

func LoadAccountsOrEmpty(path string) (*AccountStore, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return emptyAccountsStore(), nil
		}
		return nil, err
	}
	return parseAccounts(content)
}

func parseAccounts(content []byte) (*AccountStore, error) {
	if strings.TrimSpace(string(content)) == "" {
		return emptyAccountsStore(), nil
	}
	store := AccountStore{}
	if err := json.Unmarshal(content, &store); err != nil {
		return nil, err
	}
	if store.Accounts == nil {
		store.Accounts = []Account{}
	}
	return &store, nil
}

func emptyAccountsStore() *AccountStore {
	return &AccountStore{Accounts: []Account{}, NeedsSave: true}
}

func DefaultSettings() Settings {
	showBar, showPercent, showReset := true, true, true
	return Settings{UsageDisplay: "used", BarFill: "left", BarOrder: "bar_percent_reset", ShowBar: &showBar, ShowPercent: &showPercent, ShowReset: &showReset, ColorTheme: "default", AutoRefreshSeconds: 60, CompactMode: false}
}

func SaveAccounts(path string, store *AccountStore) error {
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
	store.NeedsSave = false
	return nil
}

func DefaultAccountsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return defaultAccountsRelPath
	}
	return filepath.Join(home, defaultAccountsRelPath)
}

func FindMatchingAccount(accounts []Account, candidate Account) int {
	if candidate.Provider == providers.Cursor && StringValue(candidate.AuthData.AccountID) != "" {
		for i := range accounts {
			if accounts[i].Provider == providers.Cursor && StringValue(accounts[i].AuthData.AccountID) == StringValue(candidate.AuthData.AccountID) {
				return i
			}
		}
	}
	if candidate.Email != nil {
		email := strings.ToLower(strings.TrimSpace(*candidate.Email))
		if email != "" {
			for i := range accounts {
				if accounts[i].Provider == candidate.Provider && accounts[i].Email != nil && strings.EqualFold(strings.TrimSpace(*accounts[i].Email), email) {
					return i
				}
			}
		}
	}

	return -1
}

const (
	defaultAccountsRelPath = ".config/check-usage/accounts.json"
)

func NewAccountID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("acc-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func FindAccount(accounts []Account, target string) (int, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return -1, fmt.Errorf("account target cannot be empty")
	}

	matches := make([]int, 0, 1)
	for i := range accounts {
		if accounts[i].ID == target || strings.EqualFold(strings.TrimSpace(accounts[i].Name), target) || accountEmailMatches(accounts[i], target) {
			matches = append(matches, i)
		}
	}

	switch len(matches) {
	case 0:
		return -1, fmt.Errorf("account not found: %s", target)
	case 1:
		return matches[0], nil
	default:
		return -1, fmt.Errorf("multiple accounts match %q; use account ID instead", target)
	}
}

func accountEmailMatches(account Account, target string) bool {
	return account.Email != nil && strings.EqualFold(strings.TrimSpace(*account.Email), target)
}

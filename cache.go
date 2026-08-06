package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

var usageCacheMu sync.Mutex

func usageCacheDir() string {
	root, err := os.UserCacheDir()
	if err != nil {
		return filepath.Join(".cache", "codex-usage", "accounts")
	}
	return filepath.Join(root, "codex-usage", "accounts")
}

func accountUsageCachePath(accountID string) string {
	name := base64.RawURLEncoding.EncodeToString([]byte(accountID)) + ".json"
	return filepath.Join(usageCacheDir(), name)
}

func loadAccountUsageCache(accountID string) (usageCacheEntry, bool, error) {
	usageCacheMu.Lock()
	defer usageCacheMu.Unlock()
	return loadAccountUsageCacheUnlocked(accountID)
}

func loadAccountUsageCacheUnlocked(accountID string) (usageCacheEntry, bool, error) {
	content, err := os.ReadFile(accountUsageCachePath(accountID))
	if err != nil {
		if os.IsNotExist(err) {
			return usageCacheEntry{}, false, nil
		}
		return usageCacheEntry{}, false, err
	}
	var entry usageCacheEntry
	if err := json.Unmarshal(content, &entry); err != nil {
		return usageCacheEntry{}, false, err
	}
	return entry, true, nil
}

func loadUsageCache(accounts []storedAccount) (map[string]usageCacheEntry, error) {
	cache := make(map[string]usageCacheEntry, len(accounts))
	for _, account := range accounts {
		entry, ok, err := loadAccountUsageCache(account.ID)
		if err != nil {
			return nil, err
		}
		if ok {
			cache[account.ID] = entry
		}
	}
	return cache, nil
}

func mergeAccountUsageCache(accountID string, update usageCacheEntry) error {
	usageCacheMu.Lock()
	defer usageCacheMu.Unlock()

	current, _, err := loadAccountUsageCacheUnlocked(accountID)
	if err != nil {
		return err
	}
	current.PlanType = update.PlanType
	current.RateLimit = update.RateLimit
	current.FetchedAt = update.FetchedAt
	if update.ResetCredits != nil && update.ResetFetchedAt >= current.ResetFetchedAt {
		current.ResetCredits = update.ResetCredits
		current.ResetFetchedAt = update.ResetFetchedAt
	}
	return saveAccountUsageCacheUnlocked(accountID, current)
}

func updateAccountResetCache(accountID string, payload *resetCreditsPayload, fetchedAt int64) error {
	usageCacheMu.Lock()
	defer usageCacheMu.Unlock()

	entry, _, err := loadAccountUsageCacheUnlocked(accountID)
	if err != nil {
		return err
	}
	entry.ResetCredits = payload
	entry.ResetFetchedAt = fetchedAt
	return saveAccountUsageCacheUnlocked(accountID, entry)
}

func saveAccountUsageCache(accountID string, entry usageCacheEntry) error {
	usageCacheMu.Lock()
	defer usageCacheMu.Unlock()
	return saveAccountUsageCacheUnlocked(accountID, entry)
}

func saveAccountUsageCacheUnlocked(accountID string, entry usageCacheEntry) error {
	path := accountUsageCachePath(accountID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	content, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o600)
}

func removeAccountUsageCache(accountID string) error {
	usageCacheMu.Lock()
	defer usageCacheMu.Unlock()
	err := os.Remove(accountUsageCachePath(accountID))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

package storage

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/McMelonTV/check-usage/internal/providers/codexapi"
)

var usageCacheMu sync.Mutex

func CacheDir() string {
	root, err := os.UserCacheDir()
	if err != nil {
		return filepath.Join(".cache", "check-usage", "accounts")
	}
	return filepath.Join(root, "check-usage", "accounts")
}

func AccountCachePath(accountID string) string {
	name := base64.RawURLEncoding.EncodeToString([]byte(accountID)) + ".json"
	return filepath.Join(CacheDir(), name)
}

func LoadAccountCache(accountID string) (CacheEntry, bool, error) {
	usageCacheMu.Lock()
	defer usageCacheMu.Unlock()
	return loadAccountUsageCacheUnlocked(accountID)
}

func loadAccountUsageCacheUnlocked(accountID string) (CacheEntry, bool, error) {
	content, err := os.ReadFile(AccountCachePath(accountID))
	if err != nil {
		if os.IsNotExist(err) {
			return CacheEntry{}, false, nil
		}
		return CacheEntry{}, false, err
	}
	var entry CacheEntry
	if err := json.Unmarshal(content, &entry); err != nil {
		return CacheEntry{}, false, err
	}
	return entry, true, nil
}

func LoadCache(accounts []Account) (map[string]CacheEntry, error) {
	cache := make(map[string]CacheEntry, len(accounts))
	for _, account := range accounts {
		entry, ok, err := LoadAccountCache(account.ID)
		if err != nil {
			return nil, err
		}
		if ok {
			cache[account.ID] = entry
		}
	}
	return cache, nil
}

func MergeAccountCache(accountID string, update CacheEntry) error {
	usageCacheMu.Lock()
	defer usageCacheMu.Unlock()

	current, _, err := loadAccountUsageCacheUnlocked(accountID)
	if err != nil {
		return err
	}
	if update.PlanType != "" {
		current.PlanType = update.PlanType
	}
	if update.RateLimit != nil {
		current.RateLimit = update.RateLimit
	}
	if update.FetchedAt > 0 {
		current.FetchedAt = update.FetchedAt
	}
	if update.ProviderUsage != nil {
		current.ProviderUsage = update.ProviderUsage
	}
	if update.NextFetchAt > 0 {
		current.NextFetchAt = update.NextFetchAt
	}
	if update.PlanCheckedAt > 0 {
		current.PlanCheckedAt = update.PlanCheckedAt
	}
	if update.ResetCredits != nil && update.ResetFetchedAt >= current.ResetFetchedAt {
		current.ResetCredits = update.ResetCredits
		current.ResetFetchedAt = update.ResetFetchedAt
	}
	return saveAccountUsageCacheUnlocked(accountID, current)
}

func UpdateResetCache(accountID string, payload *codexapi.ResetCreditsPayload, fetchedAt int64) error {
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

func SaveAccountCache(accountID string, entry CacheEntry) error {
	usageCacheMu.Lock()
	defer usageCacheMu.Unlock()
	return saveAccountUsageCacheUnlocked(accountID, entry)
}

func saveAccountUsageCacheUnlocked(accountID string, entry CacheEntry) error {
	path := AccountCachePath(accountID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	content, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o600)
}

func RemoveAccountCache(accountID string) error {
	usageCacheMu.Lock()
	defer usageCacheMu.Unlock()
	err := os.Remove(AccountCachePath(accountID))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

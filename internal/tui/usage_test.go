package tui

import (
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/internal/providers/codexapi"
	"github.com/McMelonTV/check-usage/internal/storage"
	"github.com/McMelonTV/check-usage/internal/usage"
)

func TestNewTUIModelBootstrapsFromCacheBeforeNetworkInit(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "accounts.json")
	shortSeconds := 5 * 60 * 60
	cachedResets := &codexapi.ResetCreditsPayload{AvailableCount: 3}
	store := &storage.AccountStore{
		Accounts: []storage.Account{{ID: "one", Name: "Personal", Provider: providers.Codex, AuthData: storage.AuthData{Type: "chatgpt"}}},
	}
	if err := storage.SaveAccounts(path, store); err != nil {
		t.Fatalf("saveAccounts() error = %v", err)
	}
	if err := storage.SaveAccountCache("one", storage.CacheEntry{
		ProviderUsage:  &providers.Usage{Plan: "plus", Metrics: []providers.Metric{usage.CodexWindowMetric(providers.SessionSlot, "SESSION", &codexapi.RateLimitDetails{PrimaryWindow: &codexapi.RateLimitWindow{UsedPercent: 35, LimitWindowSeconds: &shortSeconds}}, true), {Kind: providers.Percentage, Slot: providers.WeeklySlot, Label: "WEEKLY"}}},
		ResetCredits:   cachedResets,
		FetchedAt:      time.Now().Add(-time.Minute).Unix(),
		ResetFetchedAt: time.Now().Add(-time.Minute).Unix(),
	}); err != nil {
		t.Fatalf("saveAccountUsageCache() error = %v", err)
	}
	m := NewModel(path, &http.Client{})
	if !m.initialized || len(m.rows) != 1 || m.rows[0].Loading || len(m.rows[0].Metrics) != 2 || m.rows[0].Metrics[0].Used == nil || *m.rows[0].Metrics[0].Used != 35 {
		t.Fatalf("newTUIModel() did not bootstrap cached usage: %#v", m)
	}
	if m.resetCache["one"] == nil || m.resetCache["one"].AvailableCount != 3 {
		t.Fatalf("newTUIModel() did not bootstrap cached resets: %#v", m.resetCache)
	}
}

func TestResetCachePreservesZeroAvailableAcrossDiskRoundTrip(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if err := storage.SaveAccountCache("one", storage.CacheEntry{
		ResetCredits:   &codexapi.ResetCreditsPayload{AvailableCount: 0},
		ResetFetchedAt: time.Now().Unix(),
	}); err != nil {
		t.Fatalf("saveAccountUsageCache() error = %v", err)
	}
	entry, ok, err := storage.LoadAccountCache("one")
	if err != nil || !ok {
		t.Fatalf("loadAccountUsageCache() = ok %v, error %v", ok, err)
	}
	cached := resetPayloadCache(map[string]storage.CacheEntry{"one": entry})
	if cached["one"] == nil || cached["one"].AvailableCount != 0 {
		t.Fatalf("zero available resets were not cached: %#v", cached)
	}
}

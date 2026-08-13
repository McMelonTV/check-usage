package main

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type usageRoundTripper func(*http.Request) (*http.Response, error)

func (fn usageRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestUsageCacheUsesOneFilePerAccount(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheRoot)
	if err := saveAccountUsageCache("one", usageCacheEntry{FetchedAt: 1}); err != nil {
		t.Fatalf("save first cache: %v", err)
	}
	if err := saveAccountUsageCache("two", usageCacheEntry{FetchedAt: 2}); err != nil {
		t.Fatalf("save second cache: %v", err)
	}
	onePath, twoPath := accountUsageCachePath("one"), accountUsageCachePath("two")
	if onePath == twoPath {
		t.Fatalf("accounts share cache path %q", onePath)
	}
	for _, path := range []string{onePath, twoPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("cache file %q: %v", path, err)
		}
		if filepath.Dir(path) != filepath.Join(cacheRoot, "check-usage", "accounts") {
			t.Fatalf("cache file is outside cache directory: %q", path)
		}
	}
}

func TestDefaultProjectPathsUseCheckUsage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if !strings.Contains(defaultAccountsPath(), filepath.Join(".config", "check-usage", "accounts.json")) {
		t.Fatalf("accounts path = %q", defaultAccountsPath())
	}
	if !strings.Contains(usageCacheDir(), filepath.Join("check-usage", "accounts")) {
		t.Fatalf("cache directory = %q", usageCacheDir())
	}
}

func TestCollectUsageRowsTreatsMissingAccountsFileAsEmpty(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "missing", "accounts.json")
	rows, err := collectUsageRows(path, &http.Client{})
	if err != nil {
		t.Fatalf("collectUsageRows() error = %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("collectUsageRows() returned %d rows, want 0", len(rows))
	}
}

func TestCachedUsageRowsReturnsSkeletonWithoutSnapshot(t *testing.T) {
	accounts := []storedAccount{{ID: "one", Name: "Personal", Provider: providerCodex, AuthData: authData{Type: "chatgpt"}}}
	rows, newest := cachedUsageRows(accounts, nil, time.Now())
	if len(rows) != 1 || !rows[0].Loading {
		t.Fatalf("cachedUsageRows() = %#v", rows)
	}
	if !newest.IsZero() {
		t.Fatalf("newest cache time = %v, want zero", newest)
	}
}

func TestBaseOpenCodeRowUsesGoPlan(t *testing.T) {
	row := baseUsageRow(storedAccount{Provider: providerOpenCodeGo})
	if row.Provider != "OpenCode" || row.Plan != "Go" {
		t.Fatalf("row = %#v", row)
	}
}

func TestAccountPlanUsesOpenCodeDefault(t *testing.T) {
	if got := accountPlan(storedAccount{Provider: providerOpenCodeGo}); got != "Go" {
		t.Fatalf("plan = %q", got)
	}
}

func TestCachedUsageRowsRendersPersistedSnapshot(t *testing.T) {
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	shortSeconds := 5 * 60 * 60
	weeklySeconds := 7 * 24 * 60 * 60
	accounts := []storedAccount{{ID: "one", Name: "Personal", Provider: providerCodex, AuthData: authData{Type: "chatgpt"}}}
	cache := map[string]usageCacheEntry{"one": {
		ProviderUsage: &providerUsage{Plan: "plus", Metrics: []providerMetric{codexWindowMetric(sessionSlot, "SESSION", &rateLimitDetails{PrimaryWindow: &rateLimitWindow{UsedPercent: 25, LimitWindowSeconds: &shortSeconds}}, true), codexWindowMetric(weeklySlot, "WEEKLY", &rateLimitDetails{SecondaryWindow: &rateLimitWindow{UsedPercent: 50, LimitWindowSeconds: &weeklySeconds}}, false)}},
		ResetCredits:  &resetCreditsPayload{AvailableCount: 2},
		FetchedAt:     now.Add(-time.Minute).Unix(),
	}}
	rows, newest := cachedUsageRows(accounts, cache, now)
	if len(rows) != 1 || rows[0].Loading || rows[0].Plan != "plus" || len(rows[0].Metrics) != 2 || rows[0].Metrics[0].Used == nil || *rows[0].Metrics[0].Used != 25 || rows[0].ResetCredits != "2" {
		t.Fatalf("cachedUsageRows() = %#v", rows)
	}
	if !newest.Equal(now.Add(-time.Minute)) {
		t.Fatalf("newest cache time = %v", newest)
	}
}

func TestCachedFallbackUsageRowIsMarkedStale(t *testing.T) {
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	account := storedAccount{ID: "one", Name: "Personal", Provider: providerCodex, AuthData: authData{Type: "chatgpt"}}
	used := 25.0
	row := cachedOrUnavailableUsageRow(account, usageCacheEntry{
		ProviderUsage: &providerUsage{Metrics: []providerMetric{{Kind: percentageMetric, Slot: sessionSlot, Label: "SESSION", Used: &used}, {Kind: percentageMetric, Slot: weeklySlot, Label: "WEEKLY"}}},
		FetchedAt:     now.Add(-time.Minute).Unix(),
	}, now)
	if !row.Stale || len(row.Metrics) != 2 || row.Metrics[0].Used == nil || *row.Metrics[0].Used != 25 {
		t.Fatalf("cached fallback row = %#v", row)
	}
}

func TestNewTUIModelBootstrapsFromCacheBeforeNetworkInit(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "accounts.json")
	shortSeconds := 5 * 60 * 60
	cachedResets := &resetCreditsPayload{AvailableCount: 3}
	store := &accountsStore{
		Accounts: []storedAccount{{ID: "one", Name: "Personal", Provider: providerCodex, AuthData: authData{Type: "chatgpt"}}},
	}
	if err := saveAccounts(path, store); err != nil {
		t.Fatalf("saveAccounts() error = %v", err)
	}
	if err := saveAccountUsageCache("one", usageCacheEntry{
		ProviderUsage:  &providerUsage{Plan: "plus", Metrics: []providerMetric{codexWindowMetric(sessionSlot, "SESSION", &rateLimitDetails{PrimaryWindow: &rateLimitWindow{UsedPercent: 35, LimitWindowSeconds: &shortSeconds}}, true), {Kind: percentageMetric, Slot: weeklySlot, Label: "WEEKLY"}}},
		ResetCredits:   cachedResets,
		FetchedAt:      time.Now().Add(-time.Minute).Unix(),
		ResetFetchedAt: time.Now().Add(-time.Minute).Unix(),
	}); err != nil {
		t.Fatalf("saveAccountUsageCache() error = %v", err)
	}
	m := newTUIModel(path, &http.Client{})
	if !m.initialized || len(m.rows) != 1 || m.rows[0].Loading || len(m.rows[0].Metrics) != 2 || m.rows[0].Metrics[0].Used == nil || *m.rows[0].Metrics[0].Used != 35 {
		t.Fatalf("newTUIModel() did not bootstrap cached usage: %#v", m)
	}
	if m.resetCache["one"] == nil || m.resetCache["one"].AvailableCount != 3 {
		t.Fatalf("newTUIModel() did not bootstrap cached resets: %#v", m.resetCache)
	}
}

func TestResetCachePreservesZeroAvailableAcrossDiskRoundTrip(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if err := saveAccountUsageCache("one", usageCacheEntry{
		ResetCredits:   &resetCreditsPayload{AvailableCount: 0},
		ResetFetchedAt: time.Now().Unix(),
	}); err != nil {
		t.Fatalf("saveAccountUsageCache() error = %v", err)
	}
	entry, ok, err := loadAccountUsageCache("one")
	if err != nil || !ok {
		t.Fatalf("loadAccountUsageCache() = ok %v, error %v", ok, err)
	}
	cached := resetPayloadCache(map[string]usageCacheEntry{"one": entry})
	if cached["one"] == nil || cached["one"].AvailableCount != 0 {
		t.Fatalf("zero available resets were not cached: %#v", cached)
	}
}

func TestCollectUsageRowsDoesNotShowCachedQuotaWhenAuthenticationExpires(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	accountsPath := filepath.Join(t.TempDir(), "accounts.json")
	expiredToken := "header.eyJleHAiOjF9.signature"
	store := &accountsStore{Accounts: []storedAccount{{
		ID:       "one",
		Name:     "Personal",
		Provider: providerCodex,
		AuthData: authData{Type: "chatgpt", AccessToken: &expiredToken, RefreshToken: strPtr("expired-refresh-token")},
	}}}
	if err := saveAccounts(accountsPath, store); err != nil {
		t.Fatalf("save accounts: %v", err)
	}
	if err := saveAccountUsageCache("one", usageCacheEntry{ProviderUsage: &providerUsage{Metrics: []providerMetric{{Kind: percentageMetric, Slot: sessionSlot, Label: "SESSION"}, {Kind: percentageMetric, Slot: weeklySlot, Label: "WEEKLY"}}}, FetchedAt: time.Now().Unix()}); err != nil {
		t.Fatalf("save cache: %v", err)
	}
	client := &http.Client{Transport: usageRoundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Status:     "400 Bad Request",
			Body:       io.NopCloser(strings.NewReader(`{"error":"invalid_grant"}`)),
			Header:     make(http.Header),
		}, nil
	})}

	rows, err := collectUsageRows(accountsPath, client)
	if err != nil {
		t.Fatalf("collect usage: %v", err)
	}
	if len(rows) != 1 || !rows[0].AuthRequired || len(rows[0].Metrics) != 0 {
		t.Fatalf("expired authentication row = %#v", rows)
	}
}

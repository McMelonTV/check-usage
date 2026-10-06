package usage

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/McMelonTV/check-usage/internal/codexapi"
	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/internal/storage"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestUsageCacheUsesOneFilePerAccount(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheRoot)
	if err := storage.SaveAccountCache("one", storage.CacheEntry{FetchedAt: 1}); err != nil {
		t.Fatalf("save first cache: %v", err)
	}
	if err := storage.SaveAccountCache("two", storage.CacheEntry{FetchedAt: 2}); err != nil {
		t.Fatalf("save second cache: %v", err)
	}
	onePath, twoPath := storage.AccountCachePath("one"), storage.AccountCachePath("two")
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
	if !strings.Contains(storage.DefaultAccountsPath(), filepath.Join(".config", "check-usage", "accounts.json")) {
		t.Fatalf("accounts path = %q", storage.DefaultAccountsPath())
	}
	if !strings.Contains(storage.CacheDir(), filepath.Join("check-usage", "accounts")) {
		t.Fatalf("cache directory = %q", storage.CacheDir())
	}
}

func TestCollectUsageRowsTreatsMissingAccountsFileAsEmpty(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "missing", "accounts.json")
	rows, err := CollectRows(path, &http.Client{})
	if err != nil {
		t.Fatalf("collectUsageRows() error = %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("collectUsageRows() returned %d rows, want 0", len(rows))
	}
}

func TestCachedUsageRowsReturnsSkeletonWithoutSnapshot(t *testing.T) {
	accounts := []storage.Account{{ID: "one", Name: "Personal", Provider: providers.Codex, AuthData: storage.AuthData{Type: "chatgpt"}}}
	rows, newest := CachedRows(accounts, nil, time.Now())
	if len(rows) != 1 || !rows[0].Loading {
		t.Fatalf("cachedUsageRows() = %#v", rows)
	}
	if !newest.IsZero() {
		t.Fatalf("newest cache time = %v, want zero", newest)
	}
}

func TestBaseOpenCodeRowUsesGoPlan(t *testing.T) {
	row := BaseRow(storage.Account{Provider: providers.OpenCodeGo})
	if row.Provider != "OpenCode" || row.Plan != "Go" {
		t.Fatalf("row = %#v", row)
	}
}

func TestAccountPlanUsesOpenCodeDefault(t *testing.T) {
	if got := AccountPlan(storage.Account{Provider: providers.OpenCodeGo}); got != "Go" {
		t.Fatalf("plan = %q", got)
	}
}

func TestCachedUsageRowsRendersPersistedSnapshot(t *testing.T) {
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	shortSeconds := 5 * 60 * 60
	weeklySeconds := 7 * 24 * 60 * 60
	accounts := []storage.Account{{ID: "one", Name: "Personal", Provider: providers.Codex, AuthData: storage.AuthData{Type: "chatgpt"}}}
	cache := map[string]storage.CacheEntry{"one": {
		ProviderUsage: &providers.Usage{Plan: "plus", Metrics: []providers.Metric{CodexWindowMetric(providers.SessionSlot, "SESSION", &codexapi.RateLimitDetails{PrimaryWindow: &codexapi.RateLimitWindow{UsedPercent: 25, LimitWindowSeconds: &shortSeconds}}, true), CodexWindowMetric(providers.WeeklySlot, "WEEKLY", &codexapi.RateLimitDetails{SecondaryWindow: &codexapi.RateLimitWindow{UsedPercent: 50, LimitWindowSeconds: &weeklySeconds}}, false)}},
		ResetCredits:  &codexapi.ResetCreditsPayload{AvailableCount: 2},
		FetchedAt:     now.Add(-time.Minute).Unix(),
	}}
	rows, newest := CachedRows(accounts, cache, now)
	if len(rows) != 1 || rows[0].Loading || rows[0].Plan != "plus" || len(rows[0].Metrics) != 2 || rows[0].Metrics[0].Used == nil || *rows[0].Metrics[0].Used != 25 || rows[0].ResetCredits != "2" {
		t.Fatalf("cachedUsageRows() = %#v", rows)
	}
	if !newest.Equal(now.Add(-time.Minute)) {
		t.Fatalf("newest cache time = %v", newest)
	}
}

func TestCachedFallbackUsageRowIsMarkedStale(t *testing.T) {
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	account := storage.Account{ID: "one", Name: "Personal", Provider: providers.Codex, AuthData: storage.AuthData{Type: "chatgpt"}}
	used := 25.0
	row := CachedOrUnavailableRow(account, storage.CacheEntry{
		ProviderUsage: &providers.Usage{Metrics: []providers.Metric{{Kind: providers.Percentage, Slot: providers.SessionSlot, Label: "SESSION", Used: &used}, {Kind: providers.Percentage, Slot: providers.WeeklySlot, Label: "WEEKLY"}}},
		FetchedAt:     now.Add(-time.Minute).Unix(),
	}, now)
	if !row.Stale || len(row.Metrics) != 2 || row.Metrics[0].Used == nil || *row.Metrics[0].Used != 25 {
		t.Fatalf("cached fallback row = %#v", row)
	}
}

func TestCollectUsageRowsDoesNotShowCachedQuotaWhenAuthenticationExpires(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	accountsPath := filepath.Join(t.TempDir(), "accounts.json")
	expiredToken := "header.eyJleHAiOjF9.signature"
	store := &storage.AccountStore{Accounts: []storage.Account{{
		ID:       "one",
		Name:     "Personal",
		Provider: providers.Codex,
		AuthData: storage.AuthData{Type: "chatgpt", AccessToken: &expiredToken, RefreshToken: new("expired-refresh-token")},
	}}}
	if err := storage.SaveAccounts(accountsPath, store); err != nil {
		t.Fatalf("save accounts: %v", err)
	}
	if err := storage.SaveAccountCache("one", storage.CacheEntry{ProviderUsage: &providers.Usage{Metrics: []providers.Metric{{Kind: providers.Percentage, Slot: providers.SessionSlot, Label: "SESSION"}, {Kind: providers.Percentage, Slot: providers.WeeklySlot, Label: "WEEKLY"}}}, FetchedAt: time.Now().Unix()}); err != nil {
		t.Fatalf("save cache: %v", err)
	}
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Status:     "400 Bad Request",
			Body:       io.NopCloser(strings.NewReader(`{"error":"invalid_grant"}`)),
			Header:     make(http.Header),
		}, nil
	})}

	rows, err := CollectRows(accountsPath, client)
	if err != nil {
		t.Fatalf("collect usage: %v", err)
	}
	if len(rows) != 1 || !rows[0].AuthRequired || len(rows[0].Metrics) != 0 {
		t.Fatalf("expired authentication row = %#v", rows)
	}
}

func TestModelScopedLimitBlocking(t *testing.T) {
	full, half := 100.0, 50.0
	fable := providers.Metric{Kind: providers.Percentage, Slot: providers.MonthlySlot, Label: "FABLE", Scope: "Fable weekly", Used: &full}
	row := Row{Metrics: []providers.Metric{
		{Kind: providers.Percentage, Slot: providers.SessionSlot, Used: &half},
		{Kind: providers.Percentage, Slot: providers.WeeklySlot, Used: &half},
		fable,
	}}
	if IsSlotBlockedByLongerWindow(row, providers.SessionSlot) || IsSlotBlockedByLongerWindow(row, providers.WeeklySlot) {
		t.Fatal("exhausted model limit blocked the shared windows")
	}
	row.Metrics[1].Used = &full
	if !IsSlotBlockedByLongerWindow(row, providers.MonthlySlot) {
		t.Fatal("exhausted weekly limit did not block the model limit")
	}
	if SlotLabel(row, providers.MonthlySlot) != "FABLE WEEKLY" || !strings.HasPrefix(SlotText(row, providers.MonthlySlot, time.Now()), "✦ 100% used") {
		t.Fatalf("label = %q, text = %q", SlotLabel(row, providers.MonthlySlot), SlotText(row, providers.MonthlySlot, time.Now()))
	}
}

func TestThrottledClaudeRowUsesRecentCacheWithoutStale(t *testing.T) {
	now := time.Unix(10_000, 0)
	used := 38.0
	account := storage.Account{ID: "c", Name: "Claude", Provider: providers.Claude}
	entry := storage.CacheEntry{FetchedAt: now.Add(-2 * time.Minute).Unix(), NextFetchAt: now.Add(3 * time.Minute).Unix(),
		ProviderUsage: &providers.Usage{Metrics: []providers.Metric{{Kind: providers.Percentage, Slot: providers.SessionSlot, Used: &used}}}}
	row, ok := ThrottledRow(account, entry, now)
	if !ok || row.Stale || row.Loading || *row.Metrics[0].Used != 38 {
		t.Fatalf("recent throttled row = %#v, ok = %v", row, ok)
	}
	entry.FetchedAt = now.Add(-time.Hour).Unix()
	if row, ok := ThrottledRow(account, entry, now); !ok || !row.Stale {
		t.Fatalf("old throttled row = %#v, ok = %v", row, ok)
	}
	entry.NextFetchAt = now.Add(-time.Second).Unix()
	if _, ok := ThrottledRow(account, entry, now); ok {
		t.Fatal("throttle window over but the cache was still used")
	}
}

func TestClaudeRateLimitSetsBackoff(t *testing.T) {
	client := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		header := make(http.Header)
		header.Set("Retry-After", "900")
		return &http.Response{StatusCode: http.StatusTooManyRequests, Status: "429 Too Many Requests", Header: header, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})}
	expires := time.Now().Add(time.Hour).Unix()
	plan := "Pro"
	account := storage.Account{Provider: providers.Claude, PlanType: &plan, AuthData: storage.AuthData{AccessToken: new("a"), RefreshToken: new("r"), ExpiresAt: &expires}}
	result, err := FetchProviderUsage(t.Context(), client, account)
	if err == nil || authenticationRequired(err) || time.Until(result.NextFetchAt) < 14*time.Minute {
		t.Fatalf("next fetch = %v, err = %v", result.NextFetchAt, err)
	}
}

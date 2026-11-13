package usage

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/internal/providers/claudeapi"
	"github.com/McMelonTV/check-usage/internal/providers/codexapi"
	"github.com/McMelonTV/check-usage/internal/storage"
)

// Far-future grant end so the test does not depend on the wall clock.
const claudeUsageWithResets = `{"five_hour":{"utilization":100,"resets_at":null},"seven_day":{"utilization":40,"resets_at":null},
	"cedar_ember":{"eligible":true,"grants":[
		{"id":"g1","label":"Bonus","resets_total":3,"resets_left":2,"starts_at":"2026-11-01T00:00:00Z","ends_at":"2099-01-01T00:00:00Z"},
		{"id":"g2","label":"Old","resets_total":1,"resets_left":1,"ends_at":"2020-01-01T00:00:00Z"}]}}`

func TestRefreshClaudeResetCreditsUsesUsageAndThrottle(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	calls := 0
	client := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.String() {
		case claudeapi.ProfileURL:
			return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"organization":{"rate_limit_tier":"default_claude_max_5x"}}`))}, nil
		case claudeapi.UsageURL:
		default:
			t.Fatalf("unexpected request %s", request.URL)
		}
		calls++
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(claudeUsageWithResets))}, nil
	})}
	expires := time.Now().Add(time.Hour).Unix()
	plan := "Max 5x"
	account := storage.Account{ID: "claude-1", Name: "Claude", Provider: providers.Claude, PlanType: &plan,
		AuthData: storage.AuthData{AccessToken: new("a"), RefreshToken: new("r"), ExpiresAt: &expires}}

	_, changed, payload, err := RefreshResetCredits(account, client)
	if err != nil || changed || payload == nil || payload.AvailableCount != 2 || payload.TotalEarnedCount != 4 || calls != 1 {
		t.Fatalf("payload = %#v, changed = %v, calls = %d, err = %v", payload, changed, calls, err)
	}
	// The usage refresh was cached too, so the dashboard shows the count.
	entry, ok, err := storage.LoadAccountCache(account.ID)
	if err != nil || !ok || entry.ProviderUsage == nil || entry.ResetCredits == nil || entry.NextFetchAt <= time.Now().Unix() {
		t.Fatalf("cache = %#v, ok = %v, err = %v", entry, ok, err)
	}
	rows, _ := CachedRows([]storage.Account{account}, map[string]storage.CacheEntry{account.ID: entry}, time.Now())
	if !rows[0].SupportsResetCredits || !strings.HasPrefix(rows[0].ResetCredits, "2, exp.") || rows[0].ResetCreditsExpireAt == nil {
		t.Fatalf("row resets = %q, expiry = %v", rows[0].ResetCredits, rows[0].ResetCreditsExpireAt)
	}

	// Within the usage throttle the cached credits are returned.
	if _, _, payload, err := RefreshResetCredits(account, client); err != nil || payload.AvailableCount != 2 || calls != 1 {
		t.Fatalf("throttled payload = %#v, calls = %d, err = %v", payload, calls, err)
	}
}

func TestFetchClaudeUsageReturnsResetCredits(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(claudeUsageWithResets))}, nil
	})}
	expires := time.Now().Add(time.Hour).Unix()
	plan := "Pro"
	account := storage.Account{Provider: providers.Claude, PlanType: &plan, AuthData: storage.AuthData{AccessToken: new("a"), RefreshToken: new("r"), ExpiresAt: &expires}}
	result, err := FetchProviderUsage(t.Context(), client, account)
	if err != nil || result.ResetError != nil || result.ResetCredits == nil || result.ResetCredits.AvailableCount != 2 {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
}

func TestClaudeSurfaceRefusalDoesNotOverwriteResetBalance(t *testing.T) {
	for _, cached := range []bool{false, true} {
		t.Run(fmt.Sprint("cached=", cached), func(t *testing.T) {
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			account := storage.Account{ID: "claude", Name: "Claude", Provider: providers.Claude, PlanType: new("Pro"),
				AuthData: storage.AuthData{AccessToken: new("a"), RefreshToken: new("r"), ExpiresAt: new(time.Now().Add(time.Hour).Unix())}}
			accountsPath := filepath.Join(t.TempDir(), "accounts.json")
			if err := storage.SaveAccounts(accountsPath, &storage.AccountStore{Accounts: []storage.Account{account}}); err != nil {
				t.Fatal(err)
			}
			if cached {
				if err := storage.SaveAccountCache(account.ID, storage.CacheEntry{
					ResetCredits: &codexapi.ResetCreditsPayload{AvailableCount: 1}, ResetFetchedAt: 1,
				}); err != nil {
					t.Fatal(err)
				}
			}
			client := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
				body := `{"five_hour":{"utilization":100},"cedar_ember":{"eligible":false,"ineligible_reason":"surface","grants":[]}}`
				if request.URL.String() == claudeapi.ProfileURL {
					body = `{}`
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			rows, err := CollectRows(accountsPath, client)
			if err != nil || len(rows) != 1 {
				t.Fatalf("rows = %#v, err = %v", rows, err)
			}
			row := rows[0]
			want := "unavailable"
			if cached {
				want = "1"
			}
			if row.ResetCredits != want || row.ResetsStale != cached || *row.Metrics[0].Used != 100 {
				t.Fatalf("surface refusal lost balance or usage: %#v", row)
			}
			entry, _, err := storage.LoadAccountCache(account.ID)
			if err != nil || (!cached && entry.ResetCredits != nil) || (cached && (entry.ResetCredits == nil || entry.ResetCredits.AvailableCount != 1 || entry.ResetFetchedAt != 1)) {
				t.Fatalf("surface refusal cached a false balance: %#v, err = %v", entry, err)
			}
		})
	}
}

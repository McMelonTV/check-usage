package usage

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/internal/providers/claudeapi"
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

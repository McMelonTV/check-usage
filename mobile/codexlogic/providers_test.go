package codexlogic

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixtureClient(t *testing.T, route func(*http.Request) (int, string)) {
	t.Helper()
	old := client
	client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		code, body := route(r)
		return &http.Response{StatusCode: code, Status: http.StatusText(code), Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	t.Cleanup(func() { client = old })
}
func decodeSnapshot(t *testing.T, provider string, credentials ProviderCredentials) providerSnapshot {
	t.Helper()
	request, _ := json.Marshal(credentials)
	body, err := FetchProviderUsage(provider, string(request))
	if err != nil {
		t.Fatal(err)
	}
	var result providerSnapshot
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatal(err)
	}
	if result.Usage.Metrics == nil {
		t.Fatalf("metrics must encode as [], body: %s", body)
	}
	return result
}
func TestMobileOpenCodeRetainsAllThreeWindows(t *testing.T) {
	fixtureClient(t, func(r *http.Request) (int, string) {
		if r.URL.Path != "/zen/go/v1/usage" || r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Fatalf("unexpected request %s", r.URL.Path)
		}
		return 200, `{"usage":{"rolling":{"percent":10,"resetsAt":"2026-10-07T00:00:00Z"},"weekly":{"percent":20,"resetsAt":"2026-10-09T00:00:00Z"},"monthly":{"percent":30,"resetsAt":"2026-11-01T00:00:00Z"}}}`
	})
	result := decodeSnapshot(t, "opencode-go", ProviderCredentials{APIKey: "fixture-key"})
	if len(result.Usage.Metrics) != 3 || result.Usage.Metrics[2].Slot != "monthly" || *result.Usage.Metrics[2].Used != 30 {
		t.Fatalf("lost monthly metric: %+v", result.Usage)
	}
}
func TestMobileDeepSeekReturnsBalanceWithoutNullMetrics(t *testing.T) {
	fixtureClient(t, func(r *http.Request) (int, string) {
		return 200, `{"is_available":true,"balance_infos":[{"currency":"USD","total_balance":"12.34"}]}`
	})
	result := decodeSnapshot(t, "deepseek", ProviderCredentials{APIKey: "fixture-key"})
	if result.Usage.Plan != "USD 12.34" || len(result.Usage.Metrics) != 0 {
		t.Fatalf("bad balance: %+v", result.Usage)
	}
}
func TestMobileClaudeKeepsTokensOnRefreshFailure(t *testing.T) {
	fixtureClient(t, func(r *http.Request) (int, string) { return 401, `{"error":"invalid_grant"}` })
	credentials := ProviderCredentials{AccessToken: "original-access", RefreshToken: "original-refresh"}
	result := decodeSnapshot(t, "claude", credentials)
	if result.Credentials != credentials || !strings.HasPrefix(result.Error, "authentication required:") {
		t.Fatalf("refresh failure lost tokens or authentication state: %+v", result)
	}
}
func TestMobileClaudeReturnsRenewedTokensAndBackoffAfterUsageFailure(t *testing.T) {
	fixtureClient(t, func(r *http.Request) (int, string) {
		switch r.URL.Path {
		case "/v1/oauth/token":
			return 200, `{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`
		case "/api/oauth/profile":
			return 200, `{}`
		case "/api/oauth/usage":
			return 429, `{"error":"rate_limited"}`
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
			return 500, `{}`
		}
	})
	result := decodeSnapshot(t, "claude", ProviderCredentials{AccessToken: "old-access", RefreshToken: "old-refresh"})
	if result.Credentials.AccessToken != "new-access" || result.Credentials.RefreshToken != "new-refresh" || result.Error == "" || result.RetryAt < time.Now().Add(4*time.Minute).UnixMilli() {
		t.Fatalf("missing refreshed credentials/backoff: %+v", result)
	}
}
func TestMobileClaudeScopedWindowUsesSharedMapping(t *testing.T) {
	fixtureClient(t, func(r *http.Request) (int, string) {
		if r.URL.Path == "/api/oauth/profile" {
			return 200, `{}`
		}
		return 200, `{"limits":[{"kind":"session","percent":12},{"kind":"weekly_all","percent":34},{"kind":"weekly_scoped","percent":56,"scope":{"model":{"display_name":"Fable"}}}]}`
	})
	result := decodeSnapshot(t, "claude", ProviderCredentials{AccessToken: "access", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour).Unix()})
	if len(result.Usage.Metrics) != 3 || result.Usage.Metrics[2].Scope != "Fable weekly" {
		t.Fatalf("lost model scope: %+v", result.Usage)
	}
}
func TestMobileAPIKeyValidationAndUnknownProviders(t *testing.T) {
	for _, provider := range []string{"deepseek", "opencode-go", "cursor"} {
		if err := ValidateAPIKey(provider, "key"); err != nil {
			t.Fatal(err)
		}
		if err := ValidateAPIKey(provider, " "); err == nil {
			t.Fatal("accepted blank key")
		}
	}
	if err := ValidateAPIKey("claude", "key"); err == nil {
		t.Fatal("accepted Claude API key")
	}
	if _, err := FetchProviderUsage("unknown", `{}`); err == nil {
		t.Fatal("accepted unknown provider")
	}
	if _, err := CompleteProviderLogin("cursor", `{}`, ""); err == nil {
		t.Fatal("accepted missing Cursor session")
	}
}
func TestMobileClaudeLoginKeepsVerifierOutOfBrowserURL(t *testing.T) {
	body, err := BeginProviderLogin("claude")
	if err != nil {
		t.Fatal(err)
	}
	var session browserSession
	if err := json.Unmarshal([]byte(body), &session); err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(session.SessionID, ".")
	if len(parts) != 2 || strings.Contains(session.URL, parts[1]) || !strings.Contains(session.URL, "code_challenge=") {
		t.Fatalf("invalid PKCE browser session")
	}
}

func TestMobileCodexCachesZeroResetDetailsWithUsage(t *testing.T) {
	fixtureClient(t, func(r *http.Request) (int, string) {
		if strings.Contains(r.URL.Path, "reset") {
			return 200, `{"available_count":0,"total_earned_count":2,"credits":null}`
		}
		return 200, `{"plan_type":"plus","rate_limit":{"primary_window":{"used_percent":12}}}`
	})
	body, err := FetchSnapshot("fixture-token", "fixture-account")
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		ResetDetails *struct {
			AvailableCount int               `json:"available_count"`
			Credits        []json.RawMessage `json:"credits"`
		} `json:"reset_details"`
	}
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatal(err)
	}
	if result.ResetDetails == nil || result.ResetDetails.AvailableCount != 0 || result.ResetDetails.Credits == nil {
		t.Fatalf("zero credits were not cached: %s", body)
	}
}

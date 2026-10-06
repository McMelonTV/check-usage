package usageapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/McMelonTV/check-usage/claudeapi"
	"github.com/McMelonTV/check-usage/codexapi"
	"github.com/McMelonTV/check-usage/providers"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestListAccountsNeverReturnsCredentials(t *testing.T) {
	service := testService(t, nil)
	secret := "top-secret-token"
	if err := service.saveAccounts(&accountsStore{Accounts: []storedAccount{{
		ID: "one", Name: "Personal", Provider: providerCodex, Email: stringPointer("person@example.com"),
		AuthData: authData{Type: "chatgpt", AccessToken: &secret, RefreshToken: &secret},
	}}}); err != nil {
		t.Fatal(err)
	}

	accounts, err := service.ListAccounts()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(accounts)
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 || accounts[0].Email != "person@example.com" {
		t.Fatalf("unexpected accounts: %#v", accounts)
	}
	if strings.Contains(string(encoded), secret) || strings.Contains(string(encoded), "access_token") {
		t.Fatalf("public account response leaked credentials: %s", encoded)
	}
}

func TestDeviceAuthPersistsTokensButReturnsPublicAccount(t *testing.T) {
	idToken := testJWT(t, map[string]any{
		"email": "person@example.com",
		"exp":   4_000_000_000,
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_plan_type": "pro", "chatgpt_account_id": "remote-one",
		},
	})
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var body string
		switch request.URL.Path {
		case "/api/accounts/deviceauth/usercode":
			body = `{"device_auth_id":"session","user_code":"ABCD-EFGH","interval":"2"}`
		case "/api/accounts/deviceauth/token":
			body = `{"authorization_code":"code","code_verifier":"verifier"}`
		case "/oauth/token":
			body = `{"access_token":"access-secret","refresh_token":"refresh-secret","id_token":` + mustJSON(t, idToken) + `}`
		default:
			t.Fatalf("unexpected request: %s", request.URL)
		}
		return jsonResponse(http.StatusOK, body), nil
	})}
	service := testService(t, client)

	session, err := service.BeginDeviceAuth(context.Background(), providerCodex)
	if err != nil {
		t.Fatal(err)
	}
	if session.SessionID != "session" || session.PollIntervalSeconds != 2 {
		t.Fatalf("unexpected session: %#v", session)
	}
	result, err := service.PollDeviceAuth(context.Background(), DeviceAuthPoll{
		Provider: providerCodex, SessionID: session.SessionID, UserCode: session.UserCode,
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(result)
	if result.Status != "complete" || result.Account == nil || result.Account.PlanType != "pro" {
		t.Fatalf("unexpected auth result: %#v", result)
	}
	if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "token") {
		t.Fatalf("auth result leaked tokens: %s", encoded)
	}
	store, err := service.loadAccounts()
	if err != nil {
		t.Fatal(err)
	}
	if stringValue(store.Accounts[0].AuthData.AccessToken) != "access-secret" {
		t.Fatal("access token was not persisted")
	}
}

func TestUsageRefreshAndCachedResetCredits(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	accessToken := testJWT(t, map[string]any{"exp": now.Add(time.Hour).Unix()})
	refreshToken := "refresh"
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Authorization") != "Bearer "+accessToken {
			t.Fatalf("missing authorization header: %q", request.Header.Get("Authorization"))
		}
		switch request.URL.Path {
		case "/backend-api/wham/usage":
			return jsonResponse(http.StatusOK, `{
				"plan_type":"pro",
				"rate_limit":{
					"primary_window":{"used_percent":25,"limit_window_seconds":18000,"reset_at":2000},
					"secondary_window":{"used_percent":60,"limit_window_seconds":604800,"reset_at":3000}
				}
			}`), nil
		case "/backend-api/wham/rate-limit-reset-credits":
			return jsonResponse(http.StatusOK, `{
				"available_count":1,"total_earned_count":2,
				"credits":[
					{"status":"available","title":"Ready","expires_at":"2026-08-08T12:00:00Z"},
					{"status":"redeemed","title":"Used"}
				]
			}`), nil
		default:
			t.Fatalf("unexpected request: %s", request.URL)
		}
		return nil, nil
	})}
	service := testService(t, client)
	service.now = func() time.Time { return now }
	if err := service.saveAccounts(&accountsStore{Accounts: []storedAccount{{
		ID: "one", Name: "Personal", Provider: providerCodex,
		AuthData: authData{Type: "chatgpt", AccessToken: &accessToken, RefreshToken: &refreshToken},
	}}}); err != nil {
		t.Fatal(err)
	}

	usage, err := service.Usage(context.Background(), "one", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(usage) != 1 || usage[0].Snapshot == nil || len(usage[0].Snapshot.Windows) != 2 {
		t.Fatalf("unexpected usage: %#v", usage)
	}
	if got := *usage[0].Snapshot.Windows[0].Remaining; got != 75 {
		t.Fatalf("remaining = %v, want 75", got)
	}
	credits, err := service.ResetCredits(context.Background(), "one", false)
	if err != nil {
		t.Fatal(err)
	}
	if !credits.Cached || len(credits.Credits.Credits) != 1 || credits.Credits.Credits[0].Status != "available" {
		t.Fatalf("unexpected cached credits: %#v", credits)
	}
}

func TestUsageRefreshFallsBackToCompatibleCache(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	accessToken := testJWT(t, map[string]any{"exp": now.Add(time.Hour).Unix()})
	refreshToken := "refresh"
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusServiceUnavailable, `{"error":"temporarily unavailable"}`), nil
	})}
	service := testService(t, client)
	service.now = func() time.Time { return now }
	if err := service.saveAccounts(&accountsStore{Accounts: []storedAccount{{
		ID: "one", Name: "Personal", Provider: providerCodex,
		AuthData: authData{Type: "chatgpt", AccessToken: &accessToken, RefreshToken: &refreshToken},
	}}}); err != nil {
		t.Fatal(err)
	}
	shortSeconds := 18_000
	used := 40.0
	if err := service.saveCache("one", cacheEntry{
		PlanType: "plus", FetchedAt: now.Add(-time.Minute).Unix(),
		RateLimit: &codexapi.RateLimitDetails{PrimaryWindow: &codexapi.RateLimitWindow{
			UsedPercent: 40, LimitWindowSeconds: &shortSeconds,
		}},
		ProviderUsage: &providers.Usage{Plan: "plus", Metrics: []providers.Metric{{Kind: providers.Percentage, Slot: providers.SessionSlot, Label: "SESSION", Used: &used}}},
	}); err != nil {
		t.Fatal(err)
	}

	results, err := service.Usage(context.Background(), "one", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].Cached || results[0].Snapshot == nil || results[0].Error == "" {
		t.Fatalf("expected cached fallback with provider error: %#v", results)
	}
}

func TestSaveAPIKeyAccountAndFetchDeepSeekBalance(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://api.deepseek.com/user/balance" {
			t.Fatalf("URL = %s", request.URL)
		}
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		return jsonResponse(http.StatusOK, `{"is_available":true,"balance_infos":[{"currency":"USD","total_balance":"12.50"}]}`), nil
	})}
	service := testService(t, client)
	mutation, err := service.SaveAPIKeyAccount(APIKeyAccount{Provider: providerDeepSeek, APIKey: "secret"})
	if err != nil || mutation.Account.Provider != providerDeepSeek {
		t.Fatalf("save account = %#v, %v", mutation, err)
	}
	results, err := service.Usage(context.Background(), mutation.Account.ID, true)
	if err != nil || len(results) != 1 || len(results[0].Metrics) != 0 || results[0].Account.PlanType != "USD 12.50" {
		t.Fatalf("usage = %#v, %v", results, err)
	}
	encoded, _ := json.Marshal(mutation)
	if strings.Contains(string(encoded), "secret") {
		t.Fatalf("mutation leaked API key: %s", encoded)
	}
}

func TestSaveAPIKeyAccountUpdatesSelectedAccount(t *testing.T) {
	service := testService(t, nil)
	created, err := service.SaveAPIKeyAccount(APIKeyAccount{Provider: providerDeepSeek, APIKey: "first"})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := service.SaveAPIKeyAccount(APIKeyAccount{Account: created.Account.ID, Provider: providerDeepSeek, APIKey: "second"})
	if err != nil || updated.Action != "updated" {
		t.Fatalf("update = %#v, %v", updated, err)
	}
	store, err := service.loadAccounts()
	if err != nil || len(store.Accounts) != 1 || stringValue(store.Accounts[0].AuthData.APIKey) != "second" {
		t.Fatalf("accounts = %#v, %v", store, err)
	}
}

func TestSaveOpenCodeAccountUsesGoPlan(t *testing.T) {
	service := testService(t, nil)
	mutation, err := service.SaveAPIKeyAccount(APIKeyAccount{Provider: providerOpenCodeGo, APIKey: "secret"})
	if err != nil || mutation.Account.Name != "OpenCode" || mutation.Account.PlanType != "Go" {
		t.Fatalf("mutation = %#v, error = %v", mutation, err)
	}
}

func TestPublicOpenCodeAccountUsesDefaultPlan(t *testing.T) {
	account := storedAccount{Provider: providerOpenCodeGo}.public()
	if account.PlanType != "Go" {
		t.Fatalf("account = %#v", account)
	}
}

func testService(t *testing.T, client *http.Client) *Service {
	t.Helper()
	root := t.TempDir()
	return New(Config{
		AccountsFile: filepath.Join(root, "config", "accounts.json"),
		CacheDir:     filepath.Join(root, "cache"),
		HTTPClient:   client,
	})
}

func TestCodexProviderUsageAlwaysEmitsSessionAndWeekly(t *testing.T) {
	short, weekly := 18_000, 604_800
	reset := int64(1_000_000)
	payload := &codexapi.RateLimitStatusPayload{
		PlanType: "team",
		RateLimit: &codexapi.RateLimitDetails{
			PrimaryWindow:   &codexapi.RateLimitWindow{UsedPercent: 10, LimitWindowSeconds: &short, ResetAt: &reset},
			SecondaryWindow: &codexapi.RateLimitWindow{UsedPercent: 20, LimitWindowSeconds: &weekly, ResetAt: &reset},
		},
	}
	usage := codexProviderUsage(payload)
	if len(usage.Metrics) != 2 || usage.Metrics[0].Slot != providers.SessionSlot || usage.Metrics[1].Slot != providers.WeeklySlot {
		t.Fatalf("codex metrics = %#v, want SESSION + WEEKLY", usage.Metrics)
	}
	if usage.Metrics[0].Used == nil || usage.Metrics[1].Used == nil {
		t.Fatalf("codex metrics missing used percent: %#v", usage.Metrics)
	}
	if usage.Metrics[0].ResetAt == nil || usage.Metrics[1].ResetAt == nil {
		t.Fatalf("codex metrics missing reset times: %#v", usage.Metrics)
	}
	// A missing window still yields the other metric instead of dropping it.
	partial := &codexapi.RateLimitStatusPayload{
		RateLimit: &codexapi.RateLimitDetails{SecondaryWindow: &codexapi.RateLimitWindow{UsedPercent: 20, LimitWindowSeconds: &weekly}},
	}
	usage = codexProviderUsage(partial)
	if len(usage.Metrics) != 2 {
		t.Fatalf("partial codex metrics = %#v, want both slots present", usage.Metrics)
	}
	if usage.Metrics[1].Used == nil {
		t.Fatalf("weekly metric missing used percent: %#v", usage.Metrics)
	}
}

func TestSaveAPIKeyAccountSuffixesDuplicateNames(t *testing.T) {
	service := testService(t, nil)
	if _, err := service.SaveAPIKeyAccount(APIKeyAccount{Provider: providerOpenCodeGo, APIKey: "k1"}); err != nil {
		t.Fatal(err)
	}
	second, err := service.SaveAPIKeyAccount(APIKeyAccount{Provider: providerOpenCodeGo, APIKey: "k2"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Account.Name != "OpenCode 2" {
		t.Fatalf("duplicate account name = %q, want %q", second.Account.Name, "OpenCode 2")
	}
	third, err := service.SaveAPIKeyAccount(APIKeyAccount{Provider: providerOpenCodeGo, APIKey: "k3"})
	if err != nil {
		t.Fatal(err)
	}
	if third.Account.Name != "OpenCode 3" {
		t.Fatalf("third account name = %q, want %q", third.Account.Name, "OpenCode 3")
	}
}

func testJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status, Status: http.StatusText(status),
		Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)),
	}
}

func TestClaudeOAuthLoginAndUsagePersistTokens(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.String() {
		case claudeapi.TokenURL:
			return jsonResponse(http.StatusOK, `{"access_token":"claude-access","refresh_token":"claude-refresh","expires_in":3600,"account":{"uuid":"u1","email_address":"me@example.com"}}`), nil
		case claudeapi.ProfileURL:
			return jsonResponse(http.StatusOK, `{"organization":{"organization_type":"claude_pro"}}`), nil
		case claudeapi.UsageURL:
			return jsonResponse(http.StatusOK, `{"five_hour":{"utilization":10,"resets_at":"2026-10-06T12:00:00Z"},"seven_day":{"utilization":20,"resets_at":"2026-10-10T12:00:00Z"}}`), nil
		}
		t.Fatalf("unexpected request %s", request.URL)
		return nil, nil
	})}
	service := testService(t, client)
	if _, err := service.BeginOAuthAuth(providerCodex); err == nil {
		t.Fatal("codex accepted browser code authentication")
	}
	session, err := service.BeginOAuthAuth(providerClaude)
	if err != nil || !strings.HasPrefix(session.VerificationURL, claudeapi.AuthorizeURL) {
		t.Fatalf("session = %#v, %v", session, err)
	}
	state, _, _ := strings.Cut(session.SessionID, ".")
	result, err := service.CompleteOAuthAuth(context.Background(), OAuthComplete{Provider: providerClaude, SessionID: session.SessionID, Code: "code#" + state})
	if err != nil || result.Status != "complete" || result.Account.Email != "me@example.com" || result.Account.PlanType != "Pro" {
		t.Fatalf("complete = %#v, %v", result, err)
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), "claude-access") || strings.Contains(string(encoded), "claude-refresh") {
		t.Fatalf("result leaked tokens: %s", encoded)
	}
	usage, err := service.Usage(context.Background(), result.Account.ID, true)
	if err != nil || len(usage) != 1 || usage[0].Error != "" || len(usage[0].Metrics) != 2 || *usage[0].Metrics[1].Used != 20 {
		t.Fatalf("usage = %#v, %v", usage, err)
	}
}

func TestClaudeUsageIsThrottledAndBacksOffAfterRateLimit(t *testing.T) {
	usageCalls, limited := 0, false
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.String() {
		case claudeapi.TokenURL:
			return jsonResponse(http.StatusOK, `{"access_token":"a","refresh_token":"r","expires_in":36000,"account":{"uuid":"u","email_address":"me@example.com"}}`), nil
		case claudeapi.ProfileURL:
			return jsonResponse(http.StatusOK, `{"organization":{"organization_type":"claude_pro"}}`), nil
		case claudeapi.UsageURL:
			usageCalls++
			if limited {
				return jsonResponse(http.StatusTooManyRequests, `{}`), nil
			}
			return jsonResponse(http.StatusOK, `{"five_hour":{"utilization":10,"resets_at":null}}`), nil
		}
		t.Fatalf("unexpected request %s", request.URL)
		return nil, nil
	})}
	now := time.Unix(1_000_000, 0)
	service := testService(t, client)
	service.now = func() time.Time { return now }
	session, _ := service.BeginOAuthAuth(providerClaude)
	state, _, _ := strings.Cut(session.SessionID, ".")
	login, err := service.CompleteOAuthAuth(context.Background(), OAuthComplete{Provider: providerClaude, SessionID: session.SessionID, Code: "c#" + state})
	if err != nil {
		t.Fatal(err)
	}
	id := login.Account.ID
	if results, _ := service.Usage(context.Background(), id, true); results[0].Cached || usageCalls != 1 {
		t.Fatalf("first refresh = %#v, calls = %d", results, usageCalls)
	}
	now = now.Add(time.Minute)
	if results, _ := service.Usage(context.Background(), id, true); !results[0].Cached || results[0].Error != "" || usageCalls != 1 {
		t.Fatalf("throttled refresh = %#v, calls = %d", results, usageCalls)
	}
	now, limited = now.Add(claudeapi.MinRefreshInterval), true
	if results, _ := service.Usage(context.Background(), id, true); !results[0].Cached || !strings.Contains(results[0].Error, "Too Many Requests") || usageCalls != 2 {
		t.Fatalf("rate limited refresh = %#v, calls = %d", results, usageCalls)
	}
	now = now.Add(time.Minute)
	if results, _ := service.Usage(context.Background(), id, true); !results[0].Cached || usageCalls != 2 {
		t.Fatalf("refresh during backoff = %#v, calls = %d", results, usageCalls)
	}
}

func TestClaudePlanIsRecheckedPeriodically(t *testing.T) {
	plan, profileCalls := "claude_pro", 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.String() {
		case claudeapi.TokenURL:
			return jsonResponse(http.StatusOK, `{"access_token":"a","refresh_token":"r","expires_in":360000,"account":{"uuid":"u","email_address":"me@example.com"}}`), nil
		case claudeapi.ProfileURL:
			profileCalls++
			return jsonResponse(http.StatusOK, `{"organization":{"organization_type":"`+plan+`"}}`), nil
		case claudeapi.UsageURL:
			return jsonResponse(http.StatusOK, `{"five_hour":{"utilization":10,"resets_at":null}}`), nil
		}
		t.Fatalf("unexpected request %s", request.URL)
		return nil, nil
	})}
	now := time.Unix(1_000_000, 0)
	service := testService(t, client)
	service.now = func() time.Time { return now }
	session, _ := service.BeginOAuthAuth(providerClaude)
	state, _, _ := strings.Cut(session.SessionID, ".")
	login, err := service.CompleteOAuthAuth(context.Background(), OAuthComplete{Provider: providerClaude, SessionID: session.SessionID, Code: "c#" + state})
	if err != nil || login.Account.PlanType != "Pro" {
		t.Fatalf("login = %#v, %v", login, err)
	}
	id := login.Account.ID
	// Sign-in does not record a check time, so the first refresh checks the
	// plan once; the next one within the hour skips the profile.
	service.Usage(context.Background(), id, true)
	now = now.Add(claudeapi.MinRefreshInterval)
	service.Usage(context.Background(), id, true)
	if profileCalls != 2 {
		t.Fatalf("profile calls = %d, want sign-in plus first plan check", profileCalls)
	}
	plan = "claude_max"
	now = now.Add(claudeapi.MinRefreshInterval)
	if results, _ := service.Usage(context.Background(), id, true); results[0].Account.PlanType != "Pro" || profileCalls != 2 {
		t.Fatalf("plan rechecked too early: %#v, calls = %d", results[0].Account, profileCalls)
	}
	now = now.Add(time.Hour)
	if results, _ := service.Usage(context.Background(), id, true); results[0].Account.PlanType != "Max" || profileCalls != 3 {
		t.Fatalf("plan change not picked up: %#v, calls = %d", results[0].Account, profileCalls)
	}
}

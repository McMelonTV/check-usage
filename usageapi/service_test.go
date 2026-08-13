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

	"github.com/McMelonTV/codex-usage/codexapi"
	"github.com/McMelonTV/codex-usage/providers"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestListAccountsNeverReturnsCredentials(t *testing.T) {
	service := testService(t, nil)
	secret := "top-secret-token"
	if err := service.saveAccounts(&accountsStore{Accounts: []storedAccount{{
		ID: "one", Name: "Personal", Provider: providerOpenAICodex, Email: stringPointer("person@example.com"),
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

	session, err := service.BeginDeviceAuth(context.Background(), providerOpenAICodex)
	if err != nil {
		t.Fatal(err)
	}
	if session.SessionID != "session" || session.PollIntervalSeconds != 2 {
		t.Fatalf("unexpected session: %#v", session)
	}
	result, err := service.PollDeviceAuth(context.Background(), DeviceAuthPoll{
		Provider: providerOpenAICodex, SessionID: session.SessionID, UserCode: session.UserCode,
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
		ID: "one", Name: "Personal", Provider: providerOpenAICodex,
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
	credits, err := service.ResetCredits(context.Background(), "one", false, false)
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
		ID: "one", Name: "Personal", Provider: providerOpenAICodex,
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

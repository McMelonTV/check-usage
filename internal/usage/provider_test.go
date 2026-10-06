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

func TestFetchOpenCodeGoUsage(t *testing.T) {
	client := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://opencode.ai/zen/go/v1/usage" {
			t.Fatalf("URL = %s", request.URL)
		}
		if request.Header.Get("Authorization") != "Bearer key" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		return &http.Response{StatusCode: 200, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"usage":{"rolling":{"percent":12,"resetsAt":"2026-08-13T02:02:55.938Z"},"weekly":{"percent":35,"resetsAt":"2026-08-17T00:00:00Z"},"monthly":{"percent":50,"resetsAt":"2026-08-28T13:45:59.387Z"}}}`))}, nil
	})}
	result, err := FetchProviderUsage(t.Context(), client, storage.Account{Provider: providers.OpenCodeGo, AuthData: storage.AuthData{APIKey: new("key")}})
	if err != nil || result.Usage.Plan != "Go" || len(result.Usage.Metrics) != 3 || result.Usage.Metrics[0].Label != "SESSION" || result.Usage.Metrics[0].Used == nil || *result.Usage.Metrics[0].Used != 12 || result.Usage.Metrics[0].ResetAt == nil {
		t.Fatalf("result = %#v, %v", result, err)
	}
}

func TestOpenCodeMetricFormatsResetTime(t *testing.T) {
	used := 12.0
	reset := time.Date(2026, 8, 13, 2, 2, 55, 0, time.UTC).Unix()
	text := MetricText(providers.Metric{Kind: providers.Percentage, Label: "SESSION", Used: &used, ResetAt: &reset}, time.Date(2026, 8, 12, 2, 2, 55, 0, time.UTC))
	if strings.Contains(text, "2026-08-13T") || !strings.Contains(text, "resets in 24h") || !strings.Contains(text, "August 13, 2:02 AM UTC") {
		t.Fatalf("metric text = %q", text)
	}
}

func TestFetchDeepSeekUsage(t *testing.T) {
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"is_available":false,"balance_infos":[{"currency":"CNY","total_balance":"100.00"}]}`))}, nil
	})}
	result, err := FetchProviderUsage(t.Context(), client, storage.Account{Provider: providers.DeepSeek, AuthData: storage.AuthData{APIKey: new("key")}})
	if err != nil || result.Usage.Plan != "CNY 100.00" || len(result.Usage.Metrics) != 0 || !result.AccountChanged || storage.StringValue(result.Account.PlanType) != "CNY 100.00" {
		t.Fatalf("result = %#v, %v", result, err)
	}
}

func TestFetchClaudeUsageKeepsRotatedTokensWhenUsageFails(t *testing.T) {
	client := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		body := `{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`
		status := http.StatusOK
		if request.URL.String() != claudeapi.TokenURL {
			body, status = `{"error":"unavailable"}`, http.StatusServiceUnavailable
		}
		return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	expired := time.Now().Add(-time.Hour).Unix()
	account := storage.Account{Provider: providers.Claude, AuthData: storage.AuthData{AccessToken: new("old-access"), RefreshToken: new("old-refresh"), ExpiresAt: &expired}}
	result, err := FetchProviderUsage(t.Context(), client, account)
	if err == nil || authenticationRequired(err) {
		t.Fatalf("error = %v, want non-auth usage failure", err)
	}
	if !result.AccountChanged || storage.StringValue(result.Account.AuthData.RefreshToken) != "new-refresh" || storage.StringValue(result.Account.AuthData.AccessToken) != "new-access" || result.Account.AuthData.ExpiresAt == nil {
		t.Fatalf("result = %#v", result)
	}
}

package providers

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/McMelonTV/check-usage/internal/providers/claudeapi"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestOpenCodeDefinitionUsesGoPlan(t *testing.T) {
	definition, ok := Get(OpenCodeGo)
	if !ok || definition.Name != "OpenCode" || definition.Plan != "Go" {
		t.Fatalf("definition = %#v", definition)
	}
}

func TestCodexDefinition(t *testing.T) {
	definition, ok := Get("codex")
	if !ok || definition.ID != Codex || definition.Name != "Codex" {
		t.Fatalf("definition = %#v", definition)
	}
}

func TestOpenCodeGoRequiresCompleteWindows(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"usage":{"rolling":{"percent":0}}}`), nil
	})}
	if _, err := FetchAPIKeyUsage(t.Context(), client, OpenCodeGo, "key", "test"); err == nil {
		t.Fatal("incomplete usage response was accepted")
	}
}

func TestOpenCodeGoReportsGoPlan(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"usage":{"rolling":{"percent":10,"resetsAt":"2026-08-13T02:00:00Z"},"weekly":{"percent":20,"resetsAt":"2026-08-17T00:00:00Z"},"monthly":{"percent":30,"resetsAt":"2026-08-28T00:00:00Z"}}}`), nil
	})}
	usage, err := FetchAPIKeyUsage(t.Context(), client, OpenCodeGo, "key", "test")
	if err != nil || usage.Plan != "Go" {
		t.Fatalf("usage = %#v, error = %v", usage, err)
	}
}

func TestDeepSeekRequiresAvailabilityAndBalances(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{}`), nil
	})}
	if _, err := FetchAPIKeyUsage(t.Context(), client, DeepSeek, "key", "test"); err == nil {
		t.Fatal("incomplete balance response was accepted")
	}
}

func TestUnauthorizedIsCredentialError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusUnauthorized, `{"error":"invalid key"}`), nil
	})}
	_, err := FetchAPIKeyUsage(t.Context(), client, DeepSeek, "bad", "test")
	if !IsCredentialError(err) {
		t.Fatalf("error = %v", err)
	}
}

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func responseWithHeaders(status int, body string, headers map[string]string) *http.Response {
	result := response(status, body)
	for key, value := range headers {
		result.Header.Set(key, value)
	}
	return result
}

func TestFireworksDefinition(t *testing.T) {
	definition, ok := Get(Fireworks)
	if !ok || definition.Name != "Fireworks Nexus" || definition.Credentials != APIKey {
		t.Fatalf("definition = %#v", definition)
	}
}

func TestFetchFireworksNexusUsage(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Authorization") != "Bearer key" {
			t.Fatalf("missing authorization on %s", request.URL)
		}
		switch request.URL.Path {
		case "/verifyApiKey":
			return responseWithHeaders(http.StatusOK, "", map[string]string{
				"x-fireworks-account-id":      "accounts/acme",
				"x-fireworks-developer-email": "dev@example.com",
			}), nil
		case "/v1/accounts/acme/nexusPlans":
			if request.URL.Query().Get("pageSize") != "10" {
				t.Fatalf("plan query = %s", request.URL.RawQuery)
			}
			return response(http.StatusOK, `{"nexusPlans":[{"tier":"PRO","state":"ACTIVE"}]}`), nil
		case "/v1/accounts/acme/users":
			if request.URL.Query().Get("filter") != `email="dev@example.com"` {
				t.Fatalf("user filter = %q", request.URL.Query().Get("filter"))
			}
			return response(http.StatusOK, `{"users":[{"name":"accounts/acme/users/dev"}]}`), nil
		case "/v1/accounts/acme/users/dev/nexusPlanUsage":
			return response(http.StatusOK, `{"monthly":{"usedPercent":12.5,"endTime":"2026-12-06T00:00:00Z"},"rollingWindow":{"usedPercent":80.25,"endTime":"2026-11-28T14:48:26Z"}}`), nil
		}
		t.Fatalf("unexpected request %s", request.URL)
		return nil, nil
	})}
	usage, err := FetchAPIKeyUsage(t.Context(), client, Fireworks, "key", "test")
	if err != nil || usage.Plan != "Pro" || len(usage.Metrics) != 2 {
		t.Fatalf("usage = %#v, error = %v", usage, err)
	}
	session, monthly := usage.Metrics[0], usage.Metrics[1]
	if session.Slot != SessionSlot || session.Used == nil || *session.Used != 80.25 || session.ResetAt == nil || *session.ResetAt != time.Date(2026, 11, 28, 14, 48, 26, 0, time.UTC).Unix() {
		t.Fatalf("session = %#v", session)
	}
	if monthly.Slot != MonthlySlot || monthly.Used == nil || *monthly.Used != 12.5 || monthly.ResetAt == nil || *monthly.ResetAt != time.Date(2026, 12, 6, 0, 0, 0, 0, time.UTC).Unix() {
		t.Fatalf("monthly = %#v", monthly)
	}
}

func TestFetchFireworksNexusEnterpriseSkipsSeatWindows(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/verifyApiKey":
			return responseWithHeaders(http.StatusOK, "", map[string]string{
				"x-fireworks-account-id":      "acme",
				"x-fireworks-developer-email": "dev@example.com",
			}), nil
		case "/v1/accounts/acme/nexusPlans":
			return response(http.StatusOK, `{"nexusPlans":[{"tier":"ENTERPRISE","state":"ACTIVE"}]}`), nil
		}
		t.Fatalf("unexpected request %s", request.URL)
		return nil, nil
	})}
	usage, err := FetchAPIKeyUsage(t.Context(), client, Fireworks, "key", "test")
	if err != nil || usage.Plan != "Enterprise" || len(usage.Metrics) != 0 {
		t.Fatalf("usage = %#v, error = %v", usage, err)
	}
}

func TestFetchFireworksNexusRejectsInvalidKey(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/verifyApiKey" {
			t.Fatalf("unexpected request %s", request.URL)
		}
		return response(http.StatusUnauthorized, `{"error":"invalid key"}`), nil
	})}
	_, err := FetchAPIKeyUsage(t.Context(), client, Fireworks, "bad", "test")
	if !IsCredentialError(err) {
		t.Fatalf("error = %v", err)
	}
}

func TestFetchFireworksNexusRequiresActivePlan(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/verifyApiKey":
			return responseWithHeaders(http.StatusOK, "", map[string]string{"x-fireworks-account-id": "acme"}), nil
		case "/v1/accounts/acme/nexusPlans":
			return response(http.StatusOK, `{"nexusPlans":[{"tier":"PRO","state":"ENDED"}]}`), nil
		}
		t.Fatalf("unexpected request %s", request.URL)
		return nil, nil
	})}
	if _, err := FetchAPIKeyUsage(t.Context(), client, Fireworks, "key", "test"); err == nil {
		t.Fatal("ended plan was accepted")
	}
}

func TestFetchFireworksNexusRequiresUserEmail(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/verifyApiKey":
			return responseWithHeaders(http.StatusOK, "", map[string]string{"x-fireworks-account-id": "acme"}), nil
		case "/v1/accounts/acme/nexusPlans":
			return response(http.StatusOK, `{"nexusPlans":[{"tier":"TEAMS","state":"ACTIVE"}]}`), nil
		}
		t.Fatalf("unexpected request %s", request.URL)
		return nil, nil
	})}
	if _, err := FetchAPIKeyUsage(t.Context(), client, Fireworks, "key", "test"); err == nil {
		t.Fatal("service-account key without an email was accepted")
	}
}

func TestFetchClaudeUsageMapsWindowsAndPlan(t *testing.T) {
	now := time.Unix(1000, 0)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.String() {
		case claudeapi.UsageURL:
			return response(http.StatusOK, `{"five_hour":{"utilization":42.5,"resets_at":"2026-10-06T12:00:00.123+00:00"},"seven_day":null,"seven_day_opus":null}`), nil
		case claudeapi.ProfileURL:
			return response(http.StatusOK, `{"organization":{"organization_type":"claude_pro"}}`), nil
		}
		t.Fatalf("unexpected request %s", request.URL)
		return nil, nil
	})}
	result, err := FetchClaudeUsage(t.Context(), client, claudeapi.Credentials{AccessToken: "a", RefreshToken: "r", ExpiresAt: now.Unix() + 3600}, now, true)
	if err != nil || result.CredentialsChanged || result.Usage.Plan != "Pro" || len(result.Usage.Metrics) != 2 {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
	session, weekly := result.Usage.Metrics[0], result.Usage.Metrics[1]
	if session.Slot != SessionSlot || session.Used == nil || *session.Used != 42.5 || session.ResetAt == nil || *session.ResetAt != time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC).Unix() {
		t.Fatalf("session = %#v", session)
	}
	if weekly.Slot != WeeklySlot || weekly.Used != nil {
		t.Fatalf("weekly = %#v", weekly)
	}
}

func TestClaudeUsagePrefersLimitsArray(t *testing.T) {
	var payload claudeapi.UsagePayload
	body := `{"five_hour":{"utilization":1,"resets_at":null},"seven_day":{"utilization":2,"resets_at":null},
		"limits":[{"kind":"session","percent":58,"resets_at":"2026-10-06T12:00:00Z"},{"kind":"weekly_all","percent":14,"resets_at":"2026-10-10T12:00:00Z"},{"kind":"weekly_scoped","percent":90,"scope":{"model":{"display_name":"Fable"}}}]}`
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatal(err)
	}
	usage := ClaudeUsage(&payload)
	if *usage.Metrics[0].Used != 58 || *usage.Metrics[1].Used != 14 || usage.Metrics[1].ResetAt == nil {
		t.Fatalf("metrics = %#v", usage.Metrics)
	}
	if len(usage.Metrics) != 3 || usage.Metrics[2].Slot != MonthlySlot || usage.Metrics[2].Scope != "Fable weekly" || *usage.Metrics[2].Used != 90 {
		t.Fatalf("model metric = %#v", usage.Metrics)
	}
}

func TestClaudeModelWeeklyPrefersFable(t *testing.T) {
	var payload claudeapi.UsagePayload
	body := `{"limits":[{"kind":"weekly_scoped","percent":30,"scope":{"model":{"display_name":"Opus"}}},{"kind":"weekly_scoped","percent":70,"scope":{"model":{"display_name":"Fable"}}}]}`
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatal(err)
	}
	if model, window := payload.ModelWeekly(); model != "Fable" || *window.Utilization != 70 {
		t.Fatalf("model weekly = %q, %#v", model, window)
	}
	payload.Limits = payload.Limits[:1]
	if model, _ := payload.ModelWeekly(); model != "Opus" {
		t.Fatalf("fallback model = %q", model)
	}
	payload.Limits = nil
	if usage := ClaudeUsage(&payload); len(usage.Metrics) != 2 {
		t.Fatalf("metrics without scoped limits = %#v", usage.Metrics)
	}
}

func TestFetchClaudeUsageRefreshesAndRetriesAfterUnauthorized(t *testing.T) {
	now := time.Unix(1000, 0)
	usageCalls := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.String() {
		case claudeapi.TokenURL:
			return response(http.StatusOK, `{"access_token":"new","refresh_token":"new-refresh","expires_in":3600}`), nil
		case claudeapi.UsageURL:
			usageCalls++
			if request.Header.Get("Authorization") != "Bearer new" {
				return response(http.StatusUnauthorized, `{}`), nil
			}
			return response(http.StatusOK, `{"five_hour":{"utilization":5,"resets_at":null}}`), nil
		}
		return response(http.StatusNotFound, `{}`), nil
	})}
	result, err := FetchClaudeUsage(t.Context(), client, claudeapi.Credentials{AccessToken: "old", RefreshToken: "r", ExpiresAt: now.Unix() + 3600}, now, true)
	if err != nil || usageCalls != 2 || !result.CredentialsChanged || result.Credentials.RefreshToken != "new-refresh" || *result.Usage.Metrics[0].Used != 5 {
		t.Fatalf("result = %#v, calls = %d, error = %v", result, usageCalls, err)
	}
}

func TestFetchClaudeUsageSkipsProfileWhenPlanKnown(t *testing.T) {
	now := time.Unix(1000, 0)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != claudeapi.UsageURL {
			t.Fatalf("unexpected request %s", request.URL)
		}
		return response(http.StatusOK, `{"five_hour":{"utilization":5,"resets_at":null}}`), nil
	})}
	if _, err := FetchClaudeUsage(t.Context(), client, claudeapi.Credentials{AccessToken: "a", RefreshToken: "r", ExpiresAt: now.Unix() + 3600}, now, false); err != nil {
		t.Fatal(err)
	}
}

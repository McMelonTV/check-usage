package usageapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/McMelonTV/check-usage/internal/nativecli"
	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/internal/providers/codexapi"
)

func TestSlowNativeProbeDoesNotBlockOtherAccountsOrCatalog(t *testing.T) {
	dir := t.TempDir()
	entered, release := make(chan struct{}), make(chan struct{})
	service := New(Config{AccountsFile: filepath.Join(dir, "accounts.json"), CacheDir: filepath.Join(dir, "cache"), NativeFetch: func(ctx context.Context, provider string, _ nativecli.Config) (nativecli.Result, error) {
		if provider == providers.Antigravity {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return nativecli.Result{}, ctx.Err()
			}
		}
		return nativecli.Result{Usage: providers.Usage{Metrics: []providers.Metric{{Kind: providers.Percentage, Slot: providers.WeeklySlot, Used: new(float64(10))}}}}, nil
	}})
	slow, err := service.SaveNativeAccount(NativeAccount{Provider: providers.Antigravity, Name: "Slow", Config: nativecli.Config{Driver: "agy"}})
	if err != nil {
		t.Fatal(err)
	}
	fast, err := service.SaveNativeAccount(NativeAccount{Provider: providers.Codex, Name: "Fast", Config: nativecli.Config{Driver: "codex"}})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); _, _ = service.Usage(t.Context(), slow.Account.ID, true) }()
	<-entered
	other := make(chan error, 1)
	go func() {
		accounts, err := service.ListAccounts()
		if err == nil && len(accounts) != 2 {
			err = errors.New("catalog missing accounts")
		}
		if err == nil {
			_, err = service.Usage(t.Context(), fast.Account.ID, true)
		}
		other <- err
	}()
	select {
	case err := <-other:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		close(release)
		t.Fatal("slow provider blocked independent requests")
	}
	close(release)
	<-done
}

func TestNativeAPIFallbackIsExplicitAndSharedAcrossClients(t *testing.T) {
	var nativeCalls, apiCalls atomic.Int32
	service := testService(t, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		apiCalls.Add(1)
		return jsonResponse(200, `{"is_available":true,"balance_infos":[{"currency":"USD","total_balance":"12.34"}]}`), nil
	})})
	service.nativeFetch = func(context.Context, string, nativecli.Config) (nativecli.Result, error) {
		nativeCalls.Add(1)
		return nativecli.Result{}, errors.New("native CLI unavailable")
	}
	account, err := service.SaveAPIKeyAccount(APIKeyAccount{Provider: providers.DeepSeek, APIKey: "server-secret", Name: "Balance"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.SaveNativeAccount(NativeAccount{Account: account.Account.ID, Provider: providers.DeepSeek, Config: nativecli.Config{Driver: "json", Executable: "adapter", Fallback: false}})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = service.Usage(t.Context(), account.Account.ID, true)
	if apiCalls.Load() != 0 {
		t.Fatal("disabled fallback still sent provider requests")
	}
	_, err = service.SaveNativeAccount(NativeAccount{Account: account.Account.ID, Provider: providers.DeepSeek, Config: nativecli.Config{Driver: "json", Executable: "adapter", Fallback: true}})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		results, err := service.Usage(t.Context(), account.Account.ID, true)
		if err != nil || results[0].Error != "" || results[0].Account.PlanType != "USD 12.34" || results[0].Source != "api_fallback" {
			t.Fatalf("fallback result invalid: %v", err)
		}
	}
	if nativeCalls.Load() != 2 || apiCalls.Load() != 1 {
		t.Fatal("devices did not share fallback cache")
	}
}

func TestNativeFailurePreservesMeasurementsAndWarningDuringBackoff(t *testing.T) {
	service := testService(t, nil)
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	calls := 0
	service.nativeFetch = func(context.Context, string, nativecli.Config) (nativecli.Result, error) {
		calls++
		if calls == 2 {
			return nativecli.Result{}, errors.New("native probe failed")
		}
		return nativecli.Result{Usage: providers.Usage{Metrics: []providers.Metric{{Kind: providers.Percentage, Slot: providers.WeeklySlot, Used: new(float64(10))}}}}, nil
	}
	account, err := service.SaveNativeAccount(NativeAccount{Provider: providers.Antigravity, Config: nativecli.Config{Driver: "agy"}})
	if err != nil {
		t.Fatal(err)
	}
	results, err := service.Usage(t.Context(), account.Account.ID, true)
	if err != nil || results[0].Error != "" {
		t.Fatal("initial read failed", err)
	}
	measuredAt := results[0].FetchedAtMillis
	now = now.Add(5 * time.Minute)
	for range 2 {
		results, err = service.Usage(t.Context(), account.Account.ID, true)
		if err != nil || !results[0].Cached || results[0].Error == "" || len(results[0].Metrics) != 1 || results[0].FetchedAtMillis != measuredAt {
			t.Fatalf("failed probe lost stale measurements or warning: %#v, %v", results, err)
		}
	}
	if calls != 2 {
		t.Fatal("backoff launched another probe")
	}
	now = now.Add(5 * time.Minute)
	results, err = service.Usage(t.Context(), account.Account.ID, true)
	if err != nil || results[0].Error != "" || results[0].Cached {
		t.Fatal("recovery retained warning", err)
	}
}

func TestUnknownNativeResetTotalIsNotReportedAsZero(t *testing.T) {
	payload := &codexapi.ResetCreditsPayload{AvailableCount: 1, TotalEarnedUnknown: true, Credits: []codexapi.ResetCreditDetail{}}
	snapshot := nativeSnapshot(providers.Usage{}, payload, 1)
	if snapshot.Credits == nil || snapshot.Credits.TotalEarnedCount != nil || publicResetCredits(payload).TotalEarnedCount != nil {
		t.Fatal("native lifetime total was invented")
	}
	data, err := json.Marshal(publicResetCredits(payload))
	if err != nil || strings.Contains(string(data), "total_earned_count") {
		t.Fatal("unknown total was serialized as measured", err)
	}
}

func TestServerResetCreditsShareThrottleWithoutBlockingCatalog(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	var usageReads, creditReads atomic.Int32
	service := testService(t, &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/backend-api/wham/usage":
			usageReads.Add(1)
			return jsonResponse(200, `{"plan_type":"pro","rate_limit":{"primary_window":{"used_percent":25,"limit_window_seconds":18000}}}`), nil
		case "/backend-api/wham/rate-limit-reset-credits":
			creditReads.Add(1)
			close(entered)
			select {
			case <-release:
			case <-request.Context().Done():
				return nil, request.Context().Err()
			}
			return jsonResponse(200, `{"available_count":1,"total_earned_count":2,"credits":[]}`), nil
		default:
			return nil, errors.New("unexpected provider request")
		}
	})})
	service.minRefresh = time.Minute
	token := testJWT(t, map[string]any{"exp": time.Now().Add(time.Hour).Unix()})
	if err := service.saveAccounts(&accountsStore{Accounts: []storedAccount{{ID: "one", Name: "OpenAI", Provider: providerCodex, AuthData: authData{Type: "chatgpt", AccessToken: &token, RefreshToken: new("fixture-refresh")}}}}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := service.ResetCredits(t.Context(), "one", true); done <- err }()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("reset read did not reach the provider: %v", err)
	case <-time.After(time.Second):
		t.Fatal("reset read did not start")
	}
	catalog := make(chan error, 1)
	go func() { _, err := service.ListAccounts(); catalog <- err }()
	select {
	case err := <-catalog:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("reset provider blocked account discovery")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	credits, err := service.ResetCredits(t.Context(), "one", true)
	if err != nil || !credits.Cached || credits.Credits.AvailableCount != 1 || usageReads.Load() != 1 || creditReads.Load() != 1 {
		t.Fatalf("reset reads bypassed shared throttle: %#v, %v", credits, err)
	}
}

func TestNativeAgyPlanIsProbedSparinglyAndKeptBetweenProbes(t *testing.T) {
	service := testService(t, nil)
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	var requested []bool
	service.nativeFetch = func(_ context.Context, _ string, config nativecli.Config) (nativecli.Result, error) {
		requested = append(requested, config.FetchPlan)
		result := nativecli.Result{Usage: providers.Usage{Metrics: []providers.Metric{{Kind: providers.Percentage, Slot: providers.WeeklySlot, Used: new(float64(10))}}}, PlanChecked: config.FetchPlan}
		if config.FetchPlan {
			result.Usage.Plan = "Google AI Pro"
		}
		return result, nil
	}
	account, err := service.SaveNativeAccount(NativeAccount{Provider: providers.Antigravity, Config: nativecli.Config{Driver: "agy"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []time.Duration{0, 5 * time.Minute, 24 * time.Hour} {
		now = now.Add(step)
		results, err := service.Usage(t.Context(), account.Account.ID, true)
		if err != nil || results[0].Error != "" || results[0].Account.PlanType != "Google AI Pro" || results[0].Snapshot == nil || results[0].Snapshot.PlanType != "Google AI Pro" {
			t.Fatalf("plan missing after step %v: %#v, %v", step, results, err)
		}
	}
	if len(requested) != 3 || !requested[0] || requested[1] || !requested[2] {
		t.Fatalf("plan probe schedule = %v; want first read, skip within a day, then refresh", requested)
	}
}

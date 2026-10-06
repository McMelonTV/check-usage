package usageapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/internal/providers/codexapi"
)

// The public types must encode exactly like the internal ones they replace,
// so the JSON-RPC protocol is unchanged.
func TestPublicModelMatchesInternalJSON(t *testing.T) {
	used, reset, window := 42.5, int64(1_700_000_000), 18_000
	metrics := []providers.Metric{
		{Kind: providers.Percentage, Slot: providers.WeeklySlot, Label: "WEEKLY", Used: &used, ResetAt: &reset, Scope: "Fable"},
		{Kind: providers.Text, Slot: providers.MonthlySlot, Label: "SPEND", Text: "$3"},
	}
	credits := &codexapi.ResetCreditsPayload{AvailableCount: 1, TotalEarnedCount: 2, Credits: []codexapi.ResetCreditDetail{
		{Status: "available", Title: "Reset", GrantedAt: "2026-01-01T00:00:00Z", ExpiresAt: "2026-02-01T00:00:00Z"},
		{Status: "redeemed", Title: "Old"},
	}}
	payload := &codexapi.RateLimitStatusPayload{PlanType: "plus", RateLimit: &codexapi.RateLimitDetails{
		PrimaryWindow: &codexapi.RateLimitWindow{UsedPercent: 10, LimitWindowSeconds: &window, ResetAt: &reset},
	}}
	snapshot := codexapi.BuildSnapshot(payload, credits, nil, time.Unix(1_700_000_000, 0))

	assertSameJSON(t, publicMetrics(metrics), metrics)
	assertSameJSON(t, publicSnapshot(snapshot), snapshot)
	available := *credits
	available.Credits = credits.Credits[:1]
	assertSameJSON(t, publicResetCredits(credits), available)
}

func assertSameJSON(t *testing.T, got, want any) {
	t.Helper()
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("JSON = %s, want %s", gotJSON, wantJSON)
	}
}

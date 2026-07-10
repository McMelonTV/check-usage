package main

import (
	"strings"
	"testing"
	"time"
)

func TestResetCreditsSummaryShowsAvailableCountAndEarliestExpiry(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	payload := &resetCreditsPayload{
		AvailableCount:   2,
		TotalEarnedCount: 4,
		Credits: []resetCreditDetail{
			{Status: "redeemed", Title: "Full reset", GrantedAt: "2026-07-09T09:00:00Z", ExpiresAt: "2026-07-10T18:00:00Z"},
			{Status: "available", Title: "Full reset", GrantedAt: "2026-07-10T10:00:00Z", ExpiresAt: "2026-07-12T14:00:00Z"},
			{Status: "available", Title: "Full reset", GrantedAt: "2026-07-10T11:00:00Z", ExpiresAt: "2026-07-11T14:00:00Z"},
		},
	}

	got := resetCreditsSummary(payload, now)
	want := "2 available; earliest exp July 11, 2:00 PM UTC (26h)"
	if got != want {
		t.Fatalf("resetCreditsSummary() = %q, want %q", got, want)
	}
}

func TestResetCreditsSummaryHandlesMissingCredits(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	got := resetCreditsSummary(&resetCreditsPayload{AvailableCount: 0, TotalEarnedCount: 3}, now)
	if got != "0 available" {
		t.Fatalf("resetCreditsSummary() = %q", got)
	}
}

func TestFilteredResetCreditsExcludesUsedByDefault(t *testing.T) {
	credits := []resetCreditDetail{
		{Status: "available"},
		{Status: "redeemed"},
		{Status: "expired"},
	}

	available := filteredResetCredits(credits, false)
	if len(available) != 1 || !strings.EqualFold(available[0].Status, "available") {
		t.Fatalf("filteredResetCredits(showUsed=false) = %#v", available)
	}

	all := filteredResetCredits(credits, true)
	if len(all) != 3 {
		t.Fatalf("filteredResetCredits(showUsed=true) = %#v", all)
	}
}

func TestResetCreditsSummaryHandlesInvalidTimes(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	payload := &resetCreditsPayload{
		AvailableCount:   1,
		TotalEarnedCount: 1,
		Credits:          []resetCreditDetail{{Status: "available", GrantedAt: "not-a-date", ExpiresAt: ""}},
	}

	got := resetCreditsSummary(payload, now)
	want := "1 available"
	if got != want {
		t.Fatalf("resetCreditsSummary() = %q, want %q", got, want)
	}
}

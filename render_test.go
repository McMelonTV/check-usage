package main

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
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
	want := "2, earliest exp. in 26h (July 11, 2:00 PM UTC)"
	if got != want {
		t.Fatalf("resetCreditsSummary() = %q, want %q", got, want)
	}
}

func TestResetCreditsSummaryHandlesMissingCredits(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	got := resetCreditsSummary(&resetCreditsPayload{AvailableCount: 0, TotalEarnedCount: 3}, now)
	if got != "0" {
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
	want := "1"
	if got != want {
		t.Fatalf("resetCreditsSummary() = %q, want %q", got, want)
	}
}

func TestColorizeResetCreditsSummary(t *testing.T) {
	text := "4, earliest exp. in 26h (July 11, 2:00 PM UTC)"
	want := ansiLightGreen + "4" + ansiReset + ", earliest exp. in 26h (July 11, 2:00 PM UTC)"
	if got := colorizeResetCreditsSummary(text); got != want {
		t.Fatalf("colorizeResetCreditsSummary() = %q, want %q", got, want)
	}

	want = ansiRed + "unavailable" + ansiReset
	if got := colorizeResetCreditsSummary("unavailable"); got != want {
		t.Fatalf("colorizeResetCreditsSummary(unavailable) = %q, want %q", got, want)
	}
}

func TestApplyResetCreditStatusColors(t *testing.T) {
	table := "#  STATUS     TITLE\n1  available  Full reset\n2  expired    Full reset\n"
	credits := []resetCreditDetail{{Status: "AVAILABLE"}, {Status: "expired"}}

	got := colorizeTableOutput(applyResetCreditStatusColors(table, credits))
	want := headerText("#  STATUS     TITLE") + "\n" +
		"1  " + ansiLightGreen + "available" + ansiReset + "  Full reset\n" +
		"2  " + ansiRed + "expired" + ansiReset + "    Full reset\n"
	if got != want {
		t.Fatalf("styled reset-credit table = %q, want %q", got, want)
	}
}

func TestSelectWindowClassifiesWeeklyPrimaryByDuration(t *testing.T) {
	weeklySeconds := 7 * 24 * 60 * 60
	weekly := &rateLimitWindow{UsedPercent: 15, LimitWindowSeconds: &weeklySeconds}
	rl := &rateLimitDetails{PrimaryWindow: weekly}

	if got := selectWindow(rl, true); got != nil {
		t.Fatalf("short window = %#v, want nil", got)
	}
	if got := selectWindow(rl, false); got != weekly {
		t.Fatalf("weekly window = %#v, want primary weekly window", got)
	}
}

func TestSelectWindowClassifiesBothWindowsByDuration(t *testing.T) {
	shortSeconds := 5 * 60 * 60
	weeklySeconds := 7 * 24 * 60 * 60
	short := &rateLimitWindow{LimitWindowSeconds: &shortSeconds}
	weekly := &rateLimitWindow{LimitWindowSeconds: &weeklySeconds}
	rl := &rateLimitDetails{PrimaryWindow: short, SecondaryWindow: weekly}

	if got := selectWindow(rl, true); got != short {
		t.Fatalf("short window = %#v, want primary short window", got)
	}
	if got := selectWindow(rl, false); got != weekly {
		t.Fatalf("weekly window = %#v, want secondary weekly window", got)
	}
}

func TestUsageSlotTextUsesFixedProviderSemantics(t *testing.T) {
	deepSeek := usageRow{ProviderID: providerDeepSeek, Provider: "DeepSeek", Plan: "USD 12.50"}
	if got := usageSlotText(deepSeek, sessionSlot, time.Now()); got != "-" {
		t.Fatalf("DeepSeek session = %q", got)
	}
	if got := usageSlotText(deepSeek, weeklySlot, time.Now()); got != "-" {
		t.Fatalf("DeepSeek weekly = %q", got)
	}
	if got := resetSlotText(deepSeek); got != "-" {
		t.Fatalf("DeepSeek resets = %q", got)
	}

	codex := usageRow{ProviderID: providerOpenAICodex, Provider: "OpenAI Codex", SupportsResetCredits: true, ResetCredits: "2"}
	if got := usageSlotText(codex, monthlySlot, time.Now()); got != "-" {
		t.Fatalf("Codex monthly = %q", got)
	}
	if got := usageSlotText(codex, sessionSlot, time.Now()); got != "-" {
		t.Fatalf("Codex session = %q", got)
	}
	if got := resetSlotText(codex); got != "2" {
		t.Fatalf("Codex resets = %q", got)
	}
}

func TestRenderTableUsesFixedUsageColumns(t *testing.T) {
	used := 25.0
	rows := []usageRow{
		{Name: "Codex", ProviderID: providerOpenAICodex, Provider: "OpenAI Codex", Email: "-", Plan: "plus", Metrics: []providerMetric{{Kind: percentageMetric, Slot: sessionSlot, Label: "SESSION", Used: &used}, {Kind: percentageMetric, Slot: weeklySlot, Label: "WEEKLY", Used: &used}}, ResetCredits: "2", SupportsResetCredits: true},
		{Name: "DeepSeek", ProviderID: providerDeepSeek, Provider: "DeepSeek", Email: "-", Plan: "USD 12.50"},
	}
	output := ansi.Strip(renderTable(rows, time.Now()))
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if got := strings.Join(strings.Fields(lines[0]), " "); got != "ACCOUNT PROVIDER EMAIL PLAN SESSION WEEKLY MONTHLY RESETS" {
		t.Fatalf("table header = %q", got)
	}
	if !strings.Contains(output, "USD 12.50  -") || !strings.Contains(output, "25% used / 75% left") {
		t.Fatalf("fixed table values are missing:\n%s", output)
	}
	if strings.Count(renderTable(rows[:1], time.Now()), ansiGreen+"25%") != 2 {
		t.Fatalf("identical usage percentages were not colored independently")
	}
}

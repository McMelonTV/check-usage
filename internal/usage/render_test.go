package usage

import (
	"strings"
	"testing"
	"time"

	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/internal/providers/codexapi"
	"github.com/charmbracelet/x/ansi"
)

func TestResetCreditsSummaryShowsAvailableCountAndEarliestExpiry(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	payload := &codexapi.ResetCreditsPayload{
		AvailableCount:   2,
		TotalEarnedCount: 4,
		Credits: []codexapi.ResetCreditDetail{
			{Status: "redeemed", Title: "Full reset", GrantedAt: "2026-07-09T09:00:00Z", ExpiresAt: "2026-07-10T18:00:00Z"},
			{Status: "available", Title: "Full reset", GrantedAt: "2026-07-10T10:00:00Z", ExpiresAt: "2026-07-12T14:00:00Z"},
			{Status: "available", Title: "Full reset", GrantedAt: "2026-07-10T11:00:00Z", ExpiresAt: "2026-07-11T14:00:00Z"},
		},
	}

	got := resetCreditsSummary(payload, now)
	want := "2, exp. 1d2h · Jul 11 14:00"
	if got != want {
		t.Fatalf("resetCreditsSummary() = %q, want %q", got, want)
	}
}

func TestResetCreditsSummaryHandlesMissingCredits(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	got := resetCreditsSummary(&codexapi.ResetCreditsPayload{AvailableCount: 0, TotalEarnedCount: 3}, now)
	if got != "0" {
		t.Fatalf("resetCreditsSummary() = %q", got)
	}
}

func TestFilteredResetCreditsExcludesUnavailable(t *testing.T) {
	credits := []codexapi.ResetCreditDetail{
		{Status: "available"},
		{Status: "redeemed"},
		{Status: "expired"},
	}

	available := FilteredResetCredits(credits)
	if len(available) != 1 || !strings.EqualFold(available[0].Status, "available") {
		t.Fatalf("filteredResetCredits() = %#v", available)
	}
}

func TestResetCreditsSummaryHandlesInvalidTimes(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	payload := &codexapi.ResetCreditsPayload{
		AvailableCount:   1,
		TotalEarnedCount: 1,
		Credits:          []codexapi.ResetCreditDetail{{Status: "available", GrantedAt: "not-a-date", ExpiresAt: ""}},
	}

	got := resetCreditsSummary(payload, now)
	want := "1"
	if got != want {
		t.Fatalf("resetCreditsSummary() = %q, want %q", got, want)
	}
}

func TestColorizeResetCreditsSummary(t *testing.T) {
	text := "4, earliest exp. in 26h (July 11, 2:00 PM UTC)"
	want := ansiLightGreen + "4" + ANSIReset + ", earliest exp. in 26h (July 11, 2:00 PM UTC)"
	if got := colorizeResetCreditsSummary(text); got != want {
		t.Fatalf("colorizeResetCreditsSummary() = %q, want %q", got, want)
	}

	want = ansiRed + "unavailable" + ANSIReset
	if got := colorizeResetCreditsSummary("unavailable"); got != want {
		t.Fatalf("colorizeResetCreditsSummary(unavailable) = %q, want %q", got, want)
	}
}

func TestApplyResetCreditStatusColors(t *testing.T) {
	table := "#  STATUS     TITLE\n1  available  Full reset\n2  expired    Full reset\n"
	credits := []codexapi.ResetCreditDetail{{Status: "AVAILABLE"}, {Status: "expired"}}

	got := ColorizeTableOutput(ApplyResetCreditStatusColors(table, credits))
	want := HeaderText("#  STATUS     TITLE") + "\n" +
		"1  " + ansiLightGreen + "available" + ANSIReset + "  Full reset\n" +
		"2  " + ansiRed + "expired" + ANSIReset + "    Full reset\n"
	if got != want {
		t.Fatalf("styled reset-credit table = %q, want %q", got, want)
	}
}

func TestSelectWindowClassifiesWeeklyPrimaryByDuration(t *testing.T) {
	weeklySeconds := 7 * 24 * 60 * 60
	weekly := &codexapi.RateLimitWindow{UsedPercent: 15, LimitWindowSeconds: &weeklySeconds}
	rl := &codexapi.RateLimitDetails{PrimaryWindow: weekly}

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
	short := &codexapi.RateLimitWindow{LimitWindowSeconds: &shortSeconds}
	weekly := &codexapi.RateLimitWindow{LimitWindowSeconds: &weeklySeconds}
	rl := &codexapi.RateLimitDetails{PrimaryWindow: short, SecondaryWindow: weekly}

	if got := selectWindow(rl, true); got != short {
		t.Fatalf("short window = %#v, want primary short window", got)
	}
	if got := selectWindow(rl, false); got != weekly {
		t.Fatalf("weekly window = %#v, want secondary weekly window", got)
	}
}

func TestUsageSlotTextUsesFixedProviderSemantics(t *testing.T) {
	deepSeek := Row{ProviderID: providers.DeepSeek, Provider: "DeepSeek", Plan: "USD 12.50"}
	if got := SlotText(deepSeek, providers.SessionSlot, time.Now()); got != "-" {
		t.Fatalf("DeepSeek session = %q", got)
	}
	if got := SlotText(deepSeek, providers.WeeklySlot, time.Now()); got != "-" {
		t.Fatalf("DeepSeek weekly = %q", got)
	}
	if got := ResetSlotText(deepSeek); got != "-" {
		t.Fatalf("DeepSeek resets = %q", got)
	}

	codex := Row{ProviderID: providers.Codex, Provider: "Codex", SupportsResetCredits: true, ResetCredits: "2"}
	if got := SlotText(codex, providers.MonthlySlot, time.Now()); got != "-" {
		t.Fatalf("Codex monthly = %q", got)
	}
	if got := SlotText(codex, providers.SessionSlot, time.Now()); got != "-" {
		t.Fatalf("Codex session = %q", got)
	}
	if got := ResetSlotText(codex); got != "2" {
		t.Fatalf("Codex resets = %q", got)
	}
}

func TestRenderTableUsesFixedUsageColumns(t *testing.T) {
	used := 25.0
	now := time.Now()
	reset := now.Add(90*time.Minute + 30*time.Second).Unix()
	expiry := now.Add(26*time.Hour + 30*time.Second).Unix()
	rows := []Row{
		{Name: "Codex", ProviderID: providers.Codex, Provider: "Codex", Email: "me@example.com", Plan: "plus", Metrics: []providers.Metric{{Kind: providers.Percentage, Slot: providers.SessionSlot, Label: "SESSION", Used: &used, ResetAt: &reset}, {Kind: providers.Percentage, Slot: providers.WeeklySlot, Label: "WEEKLY", Used: &used}}, ResetCredits: "2, exp. 1d2h · Jul 11 14:00", ResetCreditsExpireAt: &expiry, SupportsResetCredits: true},
		{Name: "DeepSeek", ProviderID: providers.DeepSeek, Provider: "DeepSeek", Email: "-", Plan: "USD 12.50"},
	}
	output := ansi.Strip(RenderTable(rows, now))
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("expected header plus two lines per account:\n%s", output)
	}
	if got := strings.Join(strings.Fields(lines[0]), " "); got != "ACCOUNT PROVIDER PLAN SESSION (~5h) WEEKLY MONTHLY RESETS" {
		t.Fatalf("table header = %q", got)
	}
	if got := strings.Join(strings.Fields(lines[1]), " "); got != "Codex Codex plus 25% used / 75% left 25% used / 75% left - 2" {
		t.Fatalf("usage line = %q", got)
	}
	if got := strings.Join(strings.Fields(lines[2]), " "); got != "1h30m · "+resetDateText(&reset, now)+" 1d2h · "+resetDateText(&expiry, now) || strings.Contains(output, "@") || strings.Contains(output, "exp.") {
		t.Fatalf("subtitle line = %q", got)
	}
	if !strings.Contains(lines[3], "USD 12.50  -") {
		t.Fatalf("fixed table values are missing:\n%s", output)
	}
	if strings.Index(lines[1], "25%") != strings.Index(lines[2], "1h30m") {
		t.Fatalf("subtitle is not aligned with its usage column:\n%s", output)
	}
	if strings.Count(RenderTable(rows[:1], now), ansiGreen+"25%") != 2 {
		t.Fatalf("identical usage percentages were not colored independently")
	}
}

func TestRenderTableColorsScopedMarkerSeparately(t *testing.T) {
	used := 81.0
	rows := []Row{{Name: "Claude", ProviderID: providers.Claude, Provider: "Claude", Metrics: []providers.Metric{
		{Kind: providers.Percentage, Slot: providers.MonthlySlot, Scope: "Fable weekly", Used: &used},
	}}}
	output := RenderTable(rows, time.Now())
	marker := ansiScoped + ScopedMarker + ANSIReset
	if strings.Count(output, marker) != 2 {
		t.Fatalf("header and cell markers are not violet:\n%q", output)
	}
	if strings.Contains(output, ansiHeader+"MONTHLY "+ScopedMarker) || !strings.Contains(output, " "+colorizeUsage("81% used / 19% left", &used)) {
		t.Fatalf("marker took the header or usage color:\n%q", output)
	}
}

func TestRenderTableDropsExpiryDateToFitTerminal(t *testing.T) {
	now := time.Now()
	expiry := now.Add(26*time.Hour + 30*time.Second).Unix()
	rows := []Row{{Name: "Codex", ProviderID: providers.Codex, Provider: "Codex", ResetCredits: "2", ResetCreditsExpireAt: &expiry, SupportsResetCredits: true}}
	full := renderTableFitting(rows, now, 0)
	if !strings.Contains(ansi.Strip(full), "1d2h · "+resetDateText(&expiry, now)) {
		t.Fatalf("full table = %s", ansi.Strip(full))
	}
	narrow := renderTableFitting(rows, now, tableWidth(full)-1)
	if plain := ansi.Strip(narrow); !strings.Contains(plain, "1d2h") || strings.Contains(plain, resetDateText(&expiry, now)) || tableWidth(narrow) >= tableWidth(full) {
		t.Fatalf("narrow table = %s", plain)
	}
}

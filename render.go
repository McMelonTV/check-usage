package main

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/McMelonTV/check-usage/codexapi"
)

func printTable(rows []usageRow) {
	if len(rows) == 0 {
		fmt.Println("No accounts found.")
		return
	}
	fmt.Print(renderTable(rows, time.Now()))
}

func renderTable(rows []usageRow, now time.Time) string {
	var b bytes.Buffer
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ACCOUNT\tPROVIDER\tEMAIL\tPLAN\tSESSION\tWEEKLY\tMONTHLY\tRESETS")
	for _, row := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			row.Name, row.Provider, row.Email, row.Plan,
			usageSlotText(row, sessionSlot, now), usageSlotText(row, weeklySlot, now), usageSlotText(row, monthlySlot, now), resetSlotText(row),
		)
	}
	_ = w.Flush()
	return colorizeTableOutput(applyUsageColors(b.String(), rows, now))
}

func metricText(metric providerMetric, now time.Time) string {
	switch metric.Kind {
	case percentageMetric:
		if metric.Used == nil {
			return "-"
		}
		used := percentValue(*metric.Used)
		text := fmt.Sprintf("%.0f%% used / %.0f%% left", used, 100-used)
		if metric.ResetAt != nil {
			relative, absolute := resetTimesText(metric.ResetAt, now)
			if relative != "-" && absolute != "-" {
				text += fmt.Sprintf(" - resets in %s (%s)", relative, absolute)
			}
		}
		return text
	default:
		return "-"
	}
}

func colorizeMetric(text string, metric providerMetric) string {
	if metric.Kind != percentageMetric {
		return text
	}
	return colorizeUsage(text, metric.Used)
}

func colorizeBlockedMetric(text string, metric providerMetric) string {
	if metric.Kind != percentageMetric || text == "-" {
		return text
	}
	if metric.Used == nil {
		return text
	}
	return colorizeBlockedUsage(text, metric.Used)
}

func colorizeBlockedUsage(text string, used *float64) string {
	if used == nil || text == "-" {
		return text
	}
	return ansiBlocked + text + ansiReset
}

func applyUsageColors(tableText string, rows []usageRow, now time.Time) string {
	trimmed := strings.TrimRight(tableText, "\n")
	if trimmed == "" {
		return tableText
	}
	lines := strings.Split(trimmed, "\n")
	for index, row := range rows {
		lineIndex := index + 1
		if lineIndex >= len(lines) {
			break
		}
		line := lines[lineIndex]
		resetText := resetSlotText(row)
		line = replaceLast(line, resetText, colorizeResetCreditsSummary(resetText))
		end := len(line)
		for _, slot := range []metricSlot{monthlySlot, weeklySlot, sessionSlot} {
			metric, ok := usageMetricForSlot(row, slot)
			if !ok {
				continue
			}
			text := metricText(metric, now)
			position := strings.LastIndex(line[:end], text)
			if position < 0 {
				continue
			}
			var colored string
			if isSlotBlockedByLongerWindow(row, slot) {
				colored = colorizeBlockedMetric(text, metric)
			} else {
				colored = colorizeMetric(text, metric)
			}
			line = line[:position] + colored + line[position+len(text):]
			end = position
		}
		lines[lineIndex] = line
	}
	return strings.Join(lines, "\n") + "\n"
}

func limitSummary(rl *rateLimitDetails, primary bool, now time.Time) string {
	w := selectWindow(rl, primary)
	if w == nil {
		return "-"
	}
	used := percentValue(w.UsedPercent)
	left := 100 - used
	pctText := fmt.Sprintf("%.0f%% used / %.0f%% left", used, left)

	relative, absolute := resetTimesText(w.ResetAt, now)
	if relative == "-" || absolute == "-" {
		return pctText
	}
	return fmt.Sprintf("%s - resets in %s (%s)", pctText, relative, absolute)
}

func selectWindow(rl *rateLimitDetails, primary bool) *rateLimitWindow {
	return codexapi.SelectWindow(rl, primary)
}

func windowIsShort(window *rateLimitWindow, fallback bool) bool {
	return codexapi.WindowIsShort(window, fallback)
}

func percentValue(usedPercent float64) float64 {
	return codexapi.PercentValue(usedPercent)
}

func resetTimesText(resetAt *int64, now time.Time) (string, string) {
	if resetAt == nil {
		return "-", "-"
	}
	resetTime := time.Unix(*resetAt, 0).In(now.Location())
	return humanizeDuration(resetTime.Sub(now)), resetTime.Format("January 2, 3:04 PM MST")
}

func resetCreditsSummary(c *resetCreditsPayload, now time.Time) string {
	if c == nil {
		return "-"
	}

	summary := strconv.Itoa(c.AvailableCount)
	next, ok := earliestExpiringAvailableResetCredit(c.Credits)
	if !ok {
		return summary
	}

	expires := resetCreditTimeText(next.ExpiresAt, now, false)
	remaining := resetCreditRemainingText(next.ExpiresAt, now)
	if expires == "-" {
		return summary
	}
	return fmt.Sprintf("%s, earliest exp. in %s (%s)", summary, remaining, expires)
}

func earliestExpiringAvailableResetCredit(credits []resetCreditDetail) (resetCreditDetail, bool) {
	available := filteredResetCredits(credits, false)
	if len(available) == 0 {
		return resetCreditDetail{}, false
	}
	sortResetCredits(available)
	return available[0], true
}

func sortResetCredits(credits []resetCreditDetail) {
	sort.SliceStable(credits, func(i, j int) bool {
		left, leftOK := parseResetCreditTime(credits[i].ExpiresAt)
		right, rightOK := parseResetCreditTime(credits[j].ExpiresAt)
		switch {
		case leftOK && rightOK:
			return left.Before(right)
		case leftOK:
			return true
		case rightOK:
			return false
		default:
			return credits[i].ExpiresAt < credits[j].ExpiresAt
		}
	})
}

func resetCreditTimeText(value string, now time.Time, includeRemaining bool) string {
	t, ok := parseResetCreditTime(value)
	if !ok {
		return "-"
	}

	local := t.In(now.Location())
	text := local.Format("January 2, 3:04 PM MST")
	if includeRemaining {
		text += " (" + humanizeDuration(local.Sub(now)) + " remaining)"
	}
	return text
}

func resetCreditRemainingText(value string, now time.Time) string {
	t, ok := parseResetCreditTime(value)
	if !ok {
		return "-"
	}
	return humanizeDuration(t.In(now.Location()).Sub(now))
}

func parseResetCreditTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}

	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.999999",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
	}
	for _, format := range formats {
		if t, err := time.Parse(format, value); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func humanizeDuration(d time.Duration) string {
	if d <= 0 {
		return "now"
	}
	minutes := int(d.Round(time.Minute) / time.Minute)
	if minutes <= 0 {
		return "now"
	}
	hours := minutes / 60
	mins := minutes % 60
	if hours == 0 {
		return fmt.Sprintf("%dm", mins)
	}
	if mins == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh %dm", hours, mins)
}

func normalizeAuthType(kind string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	kind = strings.ReplaceAll(kind, "_", "")
	return kind
}

func colorizeTableOutput(tableText string) string {
	trimmed := strings.TrimRight(tableText, "\n")
	if trimmed == "" {
		return ""
	}
	lines := strings.Split(trimmed, "\n")
	lines[0] = headerText(lines[0])
	return strings.Join(lines, "\n") + "\n"
}

func headerText(text string) string {
	return ansiHeader + text + ansiReset
}

func windowUsedPercent(rl *rateLimitDetails, primary bool) *float64 {
	w := selectWindow(rl, primary)
	if w == nil {
		return nil
	}
	v := percentValue(w.UsedPercent)
	return &v
}

func colorizeUsage(text string, used *float64) string {
	if used == nil || text == "-" {
		return text
	}
	usedText := fmt.Sprintf("%.0f%%", percentValue(*used))
	coloredUsedText := usageColor(*used) + usedText + ansiReset
	return strings.Replace(text, usedText, coloredUsedText, 1)
}

func replaceLast(text, old, replacement string) string {
	index := strings.LastIndex(text, old)
	if index < 0 {
		return text
	}
	return text[:index] + replacement + text[index+len(old):]
}

func colorizeResetCreditsSummary(text string) string {
	if text == "-" {
		return text
	}
	if text == "unavailable" {
		return ansiRed + text + ansiReset
	}

	countText, _, _ := strings.Cut(text, ",")
	count, err := strconv.Atoi(strings.TrimSpace(countText))
	if err != nil {
		return text
	}
	return strings.Replace(text, countText, colorizeAvailableResetCreditCount(count), 1)
}

func colorizeAvailableResetCreditCount(count int) string {
	color := ansiLightGreen
	if count == 0 {
		color = ansiRed
	}
	return color + strconv.Itoa(count) + ansiReset
}

func colorizeResetCreditStatus(status string) string {
	color := ansiRed
	switch status {
	case "available":
		color = ansiLightGreen
	case "redeemed":
		color = ansiGreen
	case "unknown":
		color = ansiAmber
	}
	return color + status + ansiReset
}

func usageColor(used float64) string {
	used = percentValue(used)
	switch {
	case used >= 80:
		return ansiDarkRed
	case used >= 65:
		return ansiRed
	case used >= 50:
		return ansiAmber
	case used > 5:
		return ansiGreen
	default:
		return ansiLightGreen
	}
}

func resetAt(rl *rateLimitDetails, primary bool) string {
	w := selectWindow(rl, primary)
	if w == nil || w.ResetAt == nil {
		return "-"
	}
	t := time.Unix(*w.ResetAt, 0).Local()
	return t.Format("2006-01-02 15:04")
}

func creditsText(c *creditStatus) string {
	if c == nil {
		return "-"
	}
	if c.Unlimited {
		return "unlimited"
	}
	if c.Balance != nil && *c.Balance != "" {
		return *c.Balance
	}
	if c.HasCredits {
		return "yes"
	}
	return "no"
}

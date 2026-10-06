package usage

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/internal/providers/codexapi"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

const (
	ANSIReset        = "\x1b[0m"
	ansiHeader       = "\x1b[1;36m"
	ansiDarkRed      = "\x1b[1;38;2;200;20;20m"
	ansiRed          = "\x1b[38;2;239;68;68m"
	ansiAmber        = "\x1b[38;2;245;158;11m"
	ansiGreen        = "\x1b[38;2;34;197;94m"
	ansiLightGreen   = "\x1b[38;2;134;239;172m"
	ansiStrike       = "\x1b[9m"
	ansiMutedDarkRed = "\x1b[38;2;110;35;35m"
	ansiDim          = "\x1b[2m"
	ansiBlocked      = "\x1b[9;38;2;110;35;35m"
	// ansiScoped is the violet of the ✦ scoped-limit marker (the TUI's scopedColor).
	ansiScoped = "\x1b[1;38;2;179;146;240m"
)

func PrintTable(rows []Row) {
	if len(rows) == 0 {
		fmt.Println("No accounts found.")
		return
	}
	width := 0
	if term.IsTerminal(os.Stdout.Fd()) {
		if columns, _, err := term.GetSize(os.Stdout.Fd()); err == nil {
			width = columns
		}
	}
	fmt.Print(renderTableFitting(rows, time.Now(), width))
}

// renderTableFitting renders the table with full reset-credit expiry dates,
// falling back to countdowns only (as the TUI does) when that is wider than
// maxWidth. A maxWidth of zero never shortens the dates.
func renderTableFitting(rows []Row, now time.Time, maxWidth int) string {
	table := renderTableExpiry(rows, now, true)
	if maxWidth > 0 && tableWidth(table) > maxWidth {
		return renderTableExpiry(rows, now, false)
	}
	return table
}

func tableWidth(table string) int {
	widest := 0
	for _, line := range strings.Split(table, "\n") {
		widest = max(widest, ansi.StringWidth(line))
	}
	return widest
}

// RenderTable prints two lines per account, like the TUI with compact mode
// off: usage percentages on the first line and muted reset times below.
func RenderTable(rows []Row, now time.Time) string {
	return renderTableExpiry(rows, now, true)
}

// renderTableExpiry renders the table; expiryDates adds the date to each
// reset-credit expiry countdown on the second line.
func renderTableExpiry(rows []Row, now time.Time, expiryDates bool) string {
	labels := ColumnLabels
	for index, slot := range Slots {
		for _, row := range rows {
			if metric, ok := MetricForSlot(row, slot); ok && metric.IsScoped() {
				labels[index] += " " + ScopedMarker
				break
			}
		}
	}
	header := []tableCell{plainCell("ACCOUNT"), plainCell("PROVIDER"), plainCell("PLAN"), plainCell(labels[0]), plainCell(labels[1]), plainCell(labels[2]), plainCell("RESETS")}
	for i := range header {
		header[i].style = func(s string) string { return colorizeScopedMarker(s, HeaderText) }
	}
	lines := [][]tableCell{header}
	for _, row := range rows {
		slots := Slots
		main := []tableCell{plainCell(row.Name), plainCell(row.Provider), plainCell(row.Plan)}
		sub := []tableCell{plainCell(""), plainCell(""), plainCell("")}
		for _, slot := range slots {
			main = append(main, usageSlotCell(row, slot))
			sub = append(sub, dimCell(ResetSubtitleText(row, slot, now)))
		}
		count, _, _ := strings.Cut(ResetSlotText(row), ",")
		main = append(main, tableCell{text: count, style: colorizeResetCreditsSummary})
		// The earliest expiry sits under the count, like reset times under usage.
		expiry := ResetCountdownText(row.ResetCreditsExpireAt, now)
		if expiryDates && row.SupportsResetCredits {
			expiry = CreditExpirySubtitle(row, now, math.MaxInt)
		}
		sub = append(sub, dimCell(expiry))
		lines = append(lines, main, sub)
	}
	return formatTableCells(lines)
}

type tableCell struct {
	text  string
	style func(string) string
}

func plainCell(text string) tableCell { return tableCell{text: text} }

func dimCell(text string) tableCell {
	if text == "-" {
		text = ""
	}
	return tableCell{text: text, style: func(s string) string { return ansiDim + s + ANSIReset }}
}

func usageSlotCell(row Row, slot providers.MetricSlot) tableCell {
	metric, ok := MetricForSlot(row, slot)
	if !ok || metric.Kind != providers.Percentage || metric.Used == nil {
		return tableCell{text: SlotText(row, slot, time.Time{}), style: func(s string) string { return colorizeScopedMarker(s, nil) }}
	}
	used := PercentValue(*metric.Used)
	text := fmt.Sprintf("%.0f%% used / %.0f%% left", used, 100-used)
	if metric.IsScoped() {
		// The scope name is on the reset line below.
		text = ScopedMarker + " " + text
	}
	if IsSlotBlockedByLongerWindow(row, slot) {
		return tableCell{text: text, style: func(s string) string {
			return colorizeScopedMarker(s, func(rest string) string { return colorizeBlockedUsage(rest, metric.Used) })
		}}
	}
	return tableCell{text: text, style: func(s string) string {
		return colorizeScopedMarker(s, func(rest string) string { return colorizeUsage(rest, metric.Used) })
	}}
}

// colorizeScopedMarker paints any ✦ marker in s violet and the remaining text
// with style (unstyled when style is nil), so the marker never takes on the
// usage or header color.
func colorizeScopedMarker(s string, style func(string) string) string {
	if style == nil {
		style = func(text string) string { return text }
	}
	parts := strings.Split(s, ScopedMarker)
	var out strings.Builder
	for index, part := range parts {
		if index > 0 {
			out.WriteString(ansiScoped + ScopedMarker + ANSIReset)
		}
		if part != "" {
			out.WriteString(style(part))
		}
	}
	return out.String()
}

func formatTableCells(lines [][]tableCell) string {
	var widths []int
	for _, line := range lines {
		for i, c := range line {
			if i >= len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], ansi.StringWidth(c.text))
		}
	}
	var b strings.Builder
	for _, line := range lines {
		var out strings.Builder
		for i, c := range line {
			text := c.text
			if c.style != nil && text != "" {
				text = c.style(text)
			}
			out.WriteString(text)
			if i < len(line)-1 {
				out.WriteString(strings.Repeat(" ", widths[i]-ansi.StringWidth(c.text)+2))
			}
		}
		b.WriteString(strings.TrimRight(out.String(), " "))
		b.WriteString("\n")
	}
	return b.String()
}

func MetricText(metric providers.Metric, now time.Time) string {
	switch metric.Kind {
	case providers.Text:
		return FirstNonEmpty(metric.Text, "-")
	case providers.Percentage:
		if metric.Used == nil {
			return "-"
		}
		used := PercentValue(*metric.Used)
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

func colorizeBlockedUsage(text string, used *float64) string {
	if used == nil || text == "-" {
		return text
	}
	return ansiBlocked + text + ANSIReset
}

func limitSummary(rl *codexapi.RateLimitDetails, primary bool, now time.Time) string {
	w := selectWindow(rl, primary)
	if w == nil {
		return "-"
	}
	used := PercentValue(w.UsedPercent)
	left := 100 - used
	pctText := fmt.Sprintf("%.0f%% used / %.0f%% left", used, left)

	relative, absolute := resetTimesText(w.ResetAt, now)
	if relative == "-" || absolute == "-" {
		return pctText
	}
	return fmt.Sprintf("%s - resets in %s (%s)", pctText, relative, absolute)
}

func selectWindow(rl *codexapi.RateLimitDetails, primary bool) *codexapi.RateLimitWindow {
	return codexapi.SelectWindow(rl, primary)
}

func windowIsShort(window *codexapi.RateLimitWindow, fallback bool) bool {
	return codexapi.WindowIsShort(window, fallback)
}

func PercentValue(usedPercent float64) float64 {
	return codexapi.PercentValue(usedPercent)
}

func resetTimesText(resetAt *int64, now time.Time) (string, string) {
	if resetAt == nil {
		return "-", "-"
	}
	resetTime := time.Unix(*resetAt, 0).In(now.Location())
	return humanizeDuration(resetTime.Sub(now)), resetTime.Format("January 2, 3:04 PM MST")
}

func resetCreditsSummary(c *codexapi.ResetCreditsPayload, now time.Time) string {
	if c == nil {
		return "-"
	}

	summary := strconv.Itoa(c.AvailableCount)
	next, ok := earliestExpiringAvailableResetCredit(c.Credits)
	if !ok {
		return summary
	}

	expiresAt, ok := parseResetCreditTime(next.ExpiresAt)
	if !ok {
		return summary
	}
	unix := expiresAt.Unix()
	return fmt.Sprintf("%s, exp. %s · %s", summary, ResetCountdownText(&unix, now), resetDateText(&unix, now))
}

// setResetCredits fills the row's reset-credit summary and earliest expiry.
func setResetCredits(row *Row, credits *codexapi.ResetCreditsPayload, now time.Time) {
	row.ResetCredits, row.ResetCreditsExpireAt = resetCreditsSummary(credits, now), nil
	if credits == nil {
		return
	}
	if next, ok := earliestExpiringAvailableResetCredit(credits.Credits); ok {
		if expiresAt, ok := parseResetCreditTime(next.ExpiresAt); ok {
			unix := expiresAt.Unix()
			row.ResetCreditsExpireAt = &unix
		}
	}
}

func earliestExpiringAvailableResetCredit(credits []codexapi.ResetCreditDetail) (codexapi.ResetCreditDetail, bool) {
	available := FilteredResetCredits(credits)
	if len(available) == 0 {
		return codexapi.ResetCreditDetail{}, false
	}
	SortResetCredits(available)
	return available[0], true
}

func SortResetCredits(credits []codexapi.ResetCreditDetail) {
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

func ResetCreditTimeText(value string, now time.Time, includeRemaining bool) string {
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

func ResetCreditRemainingText(value string, now time.Time) string {
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

func ColorizeTableOutput(tableText string) string {
	trimmed := strings.TrimRight(tableText, "\n")
	if trimmed == "" {
		return ""
	}
	lines := strings.Split(trimmed, "\n")
	lines[0] = HeaderText(lines[0])
	return strings.Join(lines, "\n") + "\n"
}

func HeaderText(text string) string {
	return ansiHeader + text + ANSIReset
}

func windowUsedPercent(rl *codexapi.RateLimitDetails, primary bool) *float64 {
	w := selectWindow(rl, primary)
	if w == nil {
		return nil
	}
	v := PercentValue(w.UsedPercent)
	return &v
}

func colorizeUsage(text string, used *float64) string {
	if used == nil || text == "-" {
		return text
	}
	usedText := fmt.Sprintf("%.0f%%", PercentValue(*used))
	coloredUsedText := usageColor(*used) + usedText + ANSIReset
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
		return ansiRed + text + ANSIReset
	}

	countText, _, _ := strings.Cut(text, ",")
	count, err := strconv.Atoi(strings.TrimSpace(countText))
	if err != nil {
		return text
	}
	return strings.Replace(text, countText, ColorizeAvailableResetCreditCount(count), 1)
}

func ColorizeAvailableResetCreditCount(count int) string {
	color := ansiLightGreen
	if count == 0 {
		color = ansiRed
	}
	return color + strconv.Itoa(count) + ANSIReset
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
	return color + status + ANSIReset
}

func usageColor(used float64) string {
	used = PercentValue(used)
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

func resetAt(rl *codexapi.RateLimitDetails, primary bool) string {
	w := selectWindow(rl, primary)
	if w == nil || w.ResetAt == nil {
		return "-"
	}
	t := time.Unix(*w.ResetAt, 0).Local()
	return t.Format("2006-01-02 15:04")
}

func creditsText(c *codexapi.CreditStatus) string {
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

func ApplyResetCreditStatusColors(tableText string, credits []codexapi.ResetCreditDetail) string {
	trimmed := strings.TrimRight(tableText, "\n")
	if trimmed == "" {
		return tableText
	}

	lines := strings.Split(trimmed, "\n")
	rowCount := len(lines) - 1
	if rowCount > len(credits) {
		rowCount = len(credits)
	}
	for i := 0; i < rowCount; i++ {
		lineIndex := i + 1
		status := ValueOrUnknown(credits[i].Status)
		lines[lineIndex] = strings.Replace(lines[lineIndex], status, colorizeResetCreditStatus(status), 1)
	}
	return strings.Join(lines, "\n") + "\n"
}

func FilteredResetCredits(credits []codexapi.ResetCreditDetail) []codexapi.ResetCreditDetail {
	filtered := make([]codexapi.ResetCreditDetail, 0, len(credits))
	for _, credit := range credits {
		if ResetCreditAvailable(credit) {
			filtered = append(filtered, credit)
		}
	}
	return filtered
}

func ResetCreditAvailable(credit codexapi.ResetCreditDetail) bool {
	return strings.EqualFold(strings.TrimSpace(credit.Status), "available")
}

func ValueOrUnknown(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "unknown"
	}
	return strings.ReplaceAll(value, "_", " ")
}

func ValueOrDashString(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func resetDateText(resetAt *int64, now time.Time) string {
	if resetAt == nil {
		return ""
	}
	return time.Unix(*resetAt, 0).In(now.Location()).Format("Jan 2 15:04")
}

func ResetSubtitleText(row Row, slot providers.MetricSlot, now time.Time) string {
	metric, ok := MetricForSlot(row, slot)
	if !ok || (metric.ResetAt == nil && !metric.IsScoped()) {
		return ""
	}
	parts := make([]string, 0, 3)
	texts := []string{ResetCountdownText(metric.ResetAt, now), resetDateText(metric.ResetAt, now)}
	if metric.IsScoped() {
		// The scope name takes the room the date would use in a column.
		parts, texts = append(parts, metric.Scope), texts[:1]
	}
	for _, text := range texts {
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, " · ")
}

// CreditExpirySubtitle is the earliest reset-credit expiry in the same
// "countdown · date" form as usage reset subtitles, or just the countdown
// when the full text is wider than width.
func CreditExpirySubtitle(row Row, now time.Time, width int) string {
	if !row.SupportsResetCredits || row.ResetCreditsExpireAt == nil {
		return ""
	}
	countdown := ResetCountdownText(row.ResetCreditsExpireAt, now)
	full := countdown + " · " + resetDateText(row.ResetCreditsExpireAt, now)
	if lipgloss.Width(full) <= width {
		return full
	}
	return ansi.Truncate(countdown, max(0, width), "…")
}

func credentialRequiredText(row Row) string {
	if row.ProviderID == providers.Codex || row.ProviderID == providers.Claude {
		return "Sign in required"
	}
	if row.ProviderID == providers.Cursor {
		return "Sign in required"
	}
	return "API key required or invalid"
}

func ResetCountdownText(resetAt *int64, now time.Time) string {
	if resetAt == nil {
		return ""
	}
	d := time.Unix(*resetAt, 0).In(now.Location()).Sub(now)
	if d <= 0 {
		return "now"
	}
	days := int(d / (24 * time.Hour))
	hours := int((d % (24 * time.Hour)) / time.Hour)
	minutes := int((d % time.Hour) / time.Minute)
	switch {
	case days > 0:
		if hours > 0 {
			return fmt.Sprintf("%dd%dh", days, hours)
		}
		return fmt.Sprintf("%dd", days)
	case hours > 0:
		if minutes > 0 {
			return fmt.Sprintf("%dh%dm", hours, minutes)
		}
		return fmt.Sprintf("%dh", hours)
	default:
		if minutes == 0 {
			return "now"
		}
		return fmt.Sprintf("%dm", minutes)
	}
}

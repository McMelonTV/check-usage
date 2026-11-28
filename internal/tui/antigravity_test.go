package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/internal/storage"
	"github.com/McMelonTV/check-usage/internal/usage"
	"github.com/charmbracelet/x/ansi"
)

func TestAntigravityAllFourLimitsRenderAndSelectOneAccount(t *testing.T) {
	row := usage.BaseRow(storage.Account{ID: "google", Name: "Personal", Provider: providers.Antigravity})
	for i := range row.Metrics {
		row.Metrics[i].Used = new(float64(i * 25))
	}
	model := Model{width: 124, height: 40, settings: storage.DefaultSettings(), rows: []usage.Row{row, {ID: "other", Name: "Other"}}}
	for _, compact := range []bool{false, true} {
		model.settings.CompactMode = compact
		plain := ansi.Strip(usage.RenderTable([]usage.Row{row}, time.Now()))
		if !strings.Contains(plain, "Claude/GPT") || !strings.Contains(plain, "75% used / 25% left") {
			t.Fatalf("plain output lost second group: %s", plain)
		}
		wide := ansi.Strip(model.renderWideList(120, 40))
		if strings.Count(wide, "Personal") != 1 || !strings.Contains(wide, "75%") {
			t.Fatalf("wide output lost second group: %s", wide)
		}
		if strings.Contains(strings.Split(strings.TrimSpace(wide), "\n")[0], "✦") {
			t.Fatal("scope marker remains in header")
		}
		if !strings.Contains(wide, "Gemini") || !strings.Contains(wide, "Claude/GPT") {
			t.Fatal("wide output lost group labels")
		}
		compactView := ansi.Strip(model.renderCompactList(80, 40))
		for _, label := range []string{"5H USED", "WEEKLY USED", "Gemini", "Claude/GPT", "75%"} {
			if !strings.Contains(compactView, label) {
				t.Fatalf("compact missing %s: %s", label, compactView)
			}
		}
		for y := 7; y < 7+model.usageRowHeight(row, true); y++ {
			if index, ok := model.usageMouseRowIndex(y); !ok || index != 0 {
				t.Fatalf("group row selected another account at y=%d", y)
			}
		}
		if index, ok := model.usageMouseRowIndex(7 + model.usageRowHeight(row, true)); !ok || index != 1 {
			t.Fatal("following account cannot be selected")
		}
	}
	model.cursor, model.height = 1, 14
	model.settings.CompactMode = false
	start, end := model.usageVisibleRange(120, 14)
	if start != 1 || end != 2 {
		t.Fatal("tall group hid selected account")
	}
}

func TestAntigravityWeeklyExhaustionOnlyBlocksItsOwnFamily(t *testing.T) {
	row := usage.BaseRow(storage.Account{Provider: providers.Antigravity})
	for i := range row.Metrics {
		row.Metrics[i].Used = new(float64(0))
	}
	row.Metrics[1].Used = new(float64(100))
	parts := usage.MetricRows(row)
	if !usage.IsSlotBlockedByLongerWindow(parts[0], providers.SessionSlot) || usage.IsSlotBlockedByLongerWindow(parts[1], providers.SessionSlot) {
		t.Fatal("weekly exhaustion crossed model families")
	}
}

func TestUsageMouseUsesRenderedLayoutAtWidthBoundary(t *testing.T) {
	model := Model{width: 100, height: 40, settings: storage.DefaultSettings(), rows: []usage.Row{{Name: "One"}, {Name: "Two"}}}
	if index, ok := model.usageMouseRowIndex(9); !ok || index != 0 {
		t.Fatal("compact account block was treated as a wide row")
	}
	if index, ok := model.usageMouseRowIndex(11); !ok || index != 1 {
		t.Fatal("compact second account could not be selected")
	}
	model.width = 104
	if index, ok := model.usageMouseRowIndex(9); !ok || index != 1 {
		t.Fatal("wide second account could not be selected")
	}
}

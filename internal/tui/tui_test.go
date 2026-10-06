package tui

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/internal/providers/codexapi"
	"github.com/McMelonTV/check-usage/internal/storage"
	"github.com/McMelonTV/check-usage/internal/usage"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestTUIKeyboardNavigation(t *testing.T) {
	m := Model{rows: []usage.Row{{Name: "One"}, {Name: "Two"}, {Name: "Three"}}}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.cursor != 1 {
		t.Fatalf("down cursor = %d, want 1", m.cursor)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	m = updated.(Model)
	if m.cursor != 2 {
		t.Fatalf("G cursor = %d, want 2", m.cursor)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.cursor != 2 {
		t.Fatalf("cursor moved past last row: %d", m.cursor)
	}
}

func TestTUILeftAndRightSwitchTabs(t *testing.T) {
	m := Model{tab: usageTab}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(Model)
	if m.tab != usageTab {
		t.Fatalf("unfocused right selected tab %d, want usage", m.tab)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if !m.tabRowFocused {
		t.Fatal("Tab did not focus tabs")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(Model)
	if m.tab != resetsTab {
		t.Fatalf("right selected tab %d, want resets", m.tab)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = updated.(Model)
	if m.tab != usageTab {
		t.Fatalf("left selected tab %d, want usage", m.tab)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.tabRowFocused {
		t.Fatal("down did not return focus to tab content")
	}
}

func TestUsageTabSupportsAccountManagementKeys(t *testing.T) {
	m := Model{tab: usageTab,
		accounts: []storage.Account{
			{ID: "one", Name: "Personal", Provider: providers.Codex},
			{ID: "two", Name: "Work", Provider: providers.DeepSeek},
		},
		rows: []usage.Row{
			{ID: "one", Name: "Personal", ProviderID: providers.Codex},
			{ID: "two", Name: "Work", ProviderID: providers.DeepSeek},
		},
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = updated.(Model)
	if !m.editingName || m.nameInput != "Personal" {
		t.Fatalf("e did not start rename: %#v", m)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.editingName {
		t.Fatalf("esc did not cancel rename: %#v", m)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = updated.(Model)
	if m.removeArmed != "one" || !strings.Contains(m.notice, "again") {
		t.Fatalf("d did not arm removal: %#v", m)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.cursor != 1 || m.removeArmed != "" {
		t.Fatalf("cursor move did not clear armed removal: %#v", m)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = updated.(Model)
	if !m.authActive || !m.authSelectingProvider {
		t.Fatalf("a did not start provider selection: %#v", m)
	}
}

func TestUsageTabRenameTargetsRowAccountNotIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.json")
	store := &storage.AccountStore{Accounts: []storage.Account{
		{ID: "alpha", Name: "Zulu", Provider: providers.Codex},
		{ID: "beta", Name: "Alpha", Provider: providers.DeepSeek},
	}}
	if err := storage.SaveAccounts(path, store); err != nil {
		t.Fatal(err)
	}
	// rows are sorted by name, so cursor 0 points at "Alpha" (id beta).
	m := Model{tab: usageTab, accountsPath: path,
		accounts: store.Accounts,
		rows: []usage.Row{
			{ID: "beta", Name: "Alpha", ProviderID: providers.DeepSeek},
			{ID: "alpha", Name: "Zulu", ProviderID: providers.Codex},
		},
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = updated.(Model)
	m.nameInput = "Renamed"
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.editingName || cmd == nil {
		t.Fatalf("rename did not save: %#v", m)
	}
	msg := cmd().(storeSavedMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	loaded, err := storage.LoadAccounts(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Accounts[1].Name != "Renamed" {
		t.Fatalf("renamed the wrong account: %#v", loaded.Accounts)
	}
}

func TestRenameUpdatesDisplayImmediately(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "accounts.json")
	store := &storage.AccountStore{Accounts: []storage.Account{{ID: "one", Name: "Old", Provider: providers.Codex, AuthData: storage.AuthData{Type: "api_key", APIKey: new("k")}}}}
	if err := storage.SaveAccounts(path, store); err != nil {
		t.Fatal(err)
	}
	if err := storage.SaveAccountCache("one", storage.CacheEntry{FetchedAt: time.Now().Unix(), ProviderUsage: &providers.Usage{Metrics: []providers.Metric{{Kind: providers.Percentage, Slot: providers.SessionSlot, Label: "SESSION"}}}}); err != nil {
		t.Fatal(err)
	}
	m := NewModel(path, nil)
	if len(m.rows) != 1 || m.rows[0].Name != "Old" {
		t.Fatalf("initial rows: %#v", m.rows)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = updated.(Model)
	m.nameInput, m.nameCursor = "New", 3
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("no rename command")
	}
	msg := cmd().(storeSavedMsg)
	updated, _ = m.Update(msg)
	m = updated.(Model)
	if m.rows[0].Name != "New" {
		t.Fatalf("rows did not update immediately after rename: %#v", m.rows)
	}
	if m.notice != "Account renamed" {
		t.Fatalf("notice = %q", m.notice)
	}
}

func TestNameEditorSupportsCursorMovementAndSpace(t *testing.T) {
	m := Model{tab: usageTab, editingName: true, nameInput: "MyName", nameCursor: 6}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(Model)
	if m.nameInput != "MyName " || m.nameCursor != 7 {
		t.Fatalf("space insert = %q cursor %d", m.nameInput, m.nameCursor)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = updated.(Model)
	if m.nameCursor != 6 {
		t.Fatalf("left cursor = %d", m.nameCursor)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})
	m = updated.(Model)
	if m.nameInput != "MyNameX " || m.nameCursor != 7 {
		t.Fatalf("mid insert = %q cursor %d", m.nameInput, m.nameCursor)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = updated.(Model)
	if m.nameInput != "MyNaeX " || m.nameCursor != 4 {
		t.Fatalf("backspace = %q cursor %d", m.nameInput, m.nameCursor)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDelete})
	m = updated.(Model)
	if m.nameInput != "MyNaX " || m.nameCursor != 4 {
		t.Fatalf("delete = %q cursor %d", m.nameInput, m.nameCursor)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyHome})
	m = updated.(Model)
	if m.nameCursor != 0 {
		t.Fatalf("home cursor = %d", m.nameCursor)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = updated.(Model)
	if m.nameCursor != len([]rune(m.nameInput)) {
		t.Fatalf("end cursor = %d", m.nameCursor)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.editingName || m.nameInput != "" || m.nameCursor != 0 {
		t.Fatalf("esc did not reset editor: %#v", m)
	}
}

func TestTUIUpDoesNotFocusTabsAndTabPreservesSelection(t *testing.T) {
	m := Model{tab: usageTab, cursor: 2, rows: []usage.Row{{}, {}, {}}}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(Model)
	if m.tabRowFocused || m.cursor != 1 {
		t.Fatalf("Up result: focused=%v cursor=%d", m.tabRowFocused, m.cursor)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if !m.tabRowFocused || m.cursor != 1 {
		t.Fatalf("Tab changed selection: focused=%v cursor=%d", m.tabRowFocused, m.cursor)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = updated.(Model)
	if m.cursor != 1 {
		t.Fatalf("returning to Usage restored cursor %d, want 1", m.cursor)
	}
}

func TestTUITabFocusesTabRowWithoutSwitching(t *testing.T) {
	m := Model{tab: settingsTab}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if !m.tabRowFocused || m.tab != settingsTab {
		t.Fatalf("Tab result: focused=%v tab=%d", m.tabRowFocused, m.tab)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.tabRowFocused {
		t.Fatal("second Tab did not restore content focus")
	}
}

func TestTUITabFocusUpAndDownReturnToContentAndMove(t *testing.T) {
	m := Model{tab: usageTab, tabRowFocused: true, cursor: 1, rows: []usage.Row{{}, {}, {}}}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(Model)
	if m.tabRowFocused || m.cursor != 0 {
		t.Fatalf("focused Up result: focused=%v cursor=%d", m.tabRowFocused, m.cursor)
	}

	m.tabRowFocused = true
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.tabRowFocused || m.cursor != 1 {
		t.Fatalf("focused Down result: focused=%v cursor=%d", m.tabRowFocused, m.cursor)
	}
}

func TestTUIResetSidebarEnterSelectsAccountAndFocusesRows(t *testing.T) {
	cached := &codexapi.ResetCreditsPayload{AvailableCount: 2}
	m := Model{
		tab:            resetsTab,
		accounts:       []storage.Account{{ID: "one", Name: "One", Provider: providers.Codex}, {ID: "two", Name: "Two", Provider: providers.Codex}},
		resetAccountID: "one",
		resetPayload:   &codexapi.ResetCreditsPayload{},
		resetCache:     map[string]*codexapi.ResetCreditsPayload{"two": cached},
		resetLoader:    func(string) (*codexapi.ResetCreditsPayload, error) { return &codexapi.ResetCreditsPayload{}, nil },
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.cursor != 1 || m.resetRowsFocused || m.resetAccountID != "two" || m.resetPayload != cached {
		t.Fatalf("sidebar navigation: cursor=%d rowsFocused=%v account=%q payload=%p", m.cursor, m.resetRowsFocused, m.resetAccountID, m.resetPayload)
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(Model)
	if !m.resetRowsFocused || m.resetAccountID != "two" || cmd == nil {
		t.Fatalf("account selection: rowsFocused=%v account=%q cmd=%v", m.resetRowsFocused, m.resetAccountID, cmd)
	}
}

func TestTUIResetSidebarDoesNotRenderSelectedLabel(t *testing.T) {
	m := Model{
		tab: resetsTab, width: 110, height: 24,
		accounts:       []storage.Account{{ID: "one", Name: "One", Provider: providers.Codex}},
		resetAccountID: "one",
		resetPayload:   &codexapi.ResetCreditsPayload{},
	}
	if view := m.View(); strings.Contains(view, "selected") {
		t.Fatalf("reset sidebar still renders selected label:\n%s", view)
	}
}

func TestTUIUsageLoadedPreservesRowsOnRefreshError(t *testing.T) {
	m := Model{rows: []usage.Row{{Name: "Existing"}}, loading: true, initialized: true, width: 110, height: 24}
	updated, _ := m.Update(dashboardLoadedMsg{err: errors.New("offline"), at: time.Now()})
	m = updated.(Model)

	if m.loading {
		t.Fatal("model remained loading")
	}
	if len(m.rows) != 1 || m.rows[0].Name != "Existing" {
		t.Fatalf("existing rows were discarded: %#v", m.rows)
	}
	if m.err == nil {
		t.Fatal("expected refresh error")
	}
	view := m.View()
	if !strings.Contains(view, "Existing") || !strings.Contains(view, "showing last update") {
		t.Fatalf("refresh error did not preserve the dashboard:\n%s", view)
	}
}

func TestTUISettingsToggleUsageAndRefreshInterval(t *testing.T) {
	m := Model{
		tab:      settingsTab,
		settings: storage.DefaultSettings(),
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(Model)
	if m.settings.UsageDisplay != "remaining" {
		t.Fatalf("usage display = %q, want remaining", m.settings.UsageDisplay)
	}

	m.settingsCursor = 1
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(Model)
	if m.settings.BarFill != "right" {
		t.Fatalf("bar fill = %q, want right", m.settings.BarFill)
	}

	m.settingsCursor = 2
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(Model)
	if m.settings.BarOrder != "bar_reset_percent" {
		t.Fatalf("bar order = %q, want bar_reset_percent", m.settings.BarOrder)
	}

	m.settingsCursor = 3
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(Model)
	if m.settings.PercentVisible() {
		t.Fatalf("show percent remained enabled")
	}

	m.settingsCursor = 4
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(Model)
	if m.settings.ResetVisible() {
		t.Fatalf("show reset remained enabled")
	}

	m.settingsCursor = 5
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(Model)
	if m.settings.BarVisible() {
		t.Fatalf("show bar remained enabled")
	}

	m.settingsCursor = 6
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(Model)
	if m.settings.ColorTheme != "colorblind" {
		t.Fatalf("color theme = %q, want colorblind", m.settings.ColorTheme)
	}

	m.settingsCursor = 7
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(Model)
	if m.settings.AutoRefreshSeconds != 300 {
		t.Fatalf("auto refresh = %d, want 300", m.settings.AutoRefreshSeconds)
	}

	m.settingsCursor = 8
	m.settings.CompactMode = false
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(Model)
	if !m.settings.CompactMode {
		t.Fatal("compact mode remained disabled")
	}
}

func TestUsageBarFillAndPercentagePlacementAreIndependent(t *testing.T) {
	used := 40.0
	settings := storage.DefaultSettings()
	settings.BarOrder = "bar_percent_reset"
	settings.BarFill = "left"
	leftFillRightPercentage := ansi.Strip(renderUsageBar(&used, 20, false, false, false, false, settings, nil, time.Now(), false))
	settings.BarFill = "right"
	rightFillRightPercentage := ansi.Strip(renderUsageBar(&used, 20, false, false, false, false, settings, nil, time.Now(), false))
	settings.BarOrder = "percent_bar_reset"
	settings.BarFill = "left"
	leftFillLeftPercentage := ansi.Strip(renderUsageBar(&used, 20, false, false, false, false, settings, nil, time.Now(), false))
	settings.BarFill = "right"
	rightFillLeftPercentage := ansi.Strip(renderUsageBar(&used, 20, false, false, false, false, settings, nil, time.Now(), false))
	if !strings.HasPrefix(leftFillRightPercentage, "━") || !strings.HasSuffix(leftFillRightPercentage, " 40%") {
		t.Fatalf("bar first/percent last bar = %q", leftFillRightPercentage)
	}
	if !strings.HasPrefix(rightFillRightPercentage, "─") || !strings.HasSuffix(rightFillRightPercentage, " 40%") {
		t.Fatalf("right fill/percent last bar = %q", rightFillRightPercentage)
	}
	if !strings.HasPrefix(leftFillLeftPercentage, "40% ") || !strings.HasSuffix(leftFillLeftPercentage, "─") {
		t.Fatalf("percent first/bar last bar = %q", leftFillLeftPercentage)
	}
	if !strings.HasPrefix(rightFillLeftPercentage, "40% ") || !strings.HasSuffix(rightFillLeftPercentage, "━") {
		t.Fatalf("percent first/right fill bar = %q", rightFillLeftPercentage)
	}
}

func TestUsageBarLayoutTogglesAndResetCountdown(t *testing.T) {
	used := 40.0
	settings := storage.DefaultSettings()
	settings.BarOrder = "bar_percent_reset"
	resetAt := time.Now().Add(7*24*time.Hour + 3*time.Hour).Unix()
	out := ansi.Strip(renderUsageBar(&used, 24, false, false, false, false, settings, &resetAt, time.Now(), true))
	for _, want := range []string{"40%", "7d"} {
		if !strings.Contains(out, want) {
			t.Fatalf("bar missing %q: %q", want, out)
		}
	}
	if !strings.HasPrefix(out, "━") {
		t.Fatalf("bar should start with the track: %q", out)
	}

	noReset := ansi.Strip(renderUsageBar(&used, 24, false, false, false, false, settings, &resetAt, time.Now(), false))
	if strings.Contains(noReset, "7d") {
		t.Fatalf("reset countdown shown without includeReset: %q", noReset)
	}

	settings.ShowPercent = new(false)
	noPercent := ansi.Strip(renderUsageBar(&used, 24, false, false, false, false, settings, &resetAt, time.Now(), true))
	if strings.Contains(noPercent, "40%") {
		t.Fatalf("percentage shown after disable: %q", noPercent)
	}
	if !strings.Contains(noPercent, "7d") {
		t.Fatalf("countdown missing after percent disabled: %q", noPercent)
	}

	settings.ShowPercent = new(true)
	settings.ShowBar = new(false)
	noBar := ansi.Strip(renderUsageBar(&used, 24, false, false, false, false, settings, &resetAt, time.Now(), true))
	if strings.Contains(noBar, "━") || strings.Contains(noBar, "─") {
		t.Fatalf("bar shown after disable: %q", noBar)
	}
	if !strings.Contains(noBar, "40%") || !strings.Contains(noBar, "7d") {
		t.Fatalf("percent/countdown lost with bar hidden: %q", noBar)
	}
}

func TestUsageListLabelsStaleFallbackRows(t *testing.T) {
	used := 25.0
	m := Model{
		width:    110,
		height:   24,
		settings: storage.DefaultSettings(),
		rows:     []usage.Row{{Name: "Personal", ProviderID: providers.Codex, Provider: "Codex", Plan: "free", Metrics: []providers.Metric{{Kind: providers.Percentage, Slot: providers.SessionSlot, Label: "SESSION", Used: &used}, {Kind: providers.Percentage, Slot: providers.WeeklySlot, Label: "WEEKLY", Used: &used}}, Stale: true}},
	}
	if view := ansi.Strip(m.renderUsageTab(106, 22)); !strings.Contains(view, "(Stale)") {
		t.Fatalf("stale usage row is not labeled:\n%s", view)
	}
}

func TestUsageListShowsAuthenticationRecovery(t *testing.T) {
	m := Model{
		width: 110, height: 24, settings: storage.DefaultSettings(),
		rows: []usage.Row{{Name: "Personal", ProviderID: providers.Codex, Provider: "Codex", Plan: "free", AuthRequired: true}},
	}
	view := ansi.Strip(m.renderUsageTab(106, 22))
	if !strings.Contains(strings.ToLower(view), "sign in required") {
		t.Fatalf("authentication status is missing:\n%s", view)
	}
}

func TestTUIMouseClickChangesTabAndSelectsUsageRow(t *testing.T) {
	m := Model{width: 100, height: 24, settings: storage.DefaultSettings(), rows: []usage.Row{{Name: "One"}, {Name: "Two"}}}
	updated, _ := m.Update(tea.MouseMsg{X: 12, Y: 2, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(Model)
	if m.tab != resetsTab {
		t.Fatalf("mouse tab = %d, want resets", m.tab)
	}
	m.tab = usageTab
	updated, _ = m.Update(tea.MouseMsg{X: 4, Y: 9, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(Model)
	if m.cursor != 1 {
		t.Fatalf("mouse row = %d, want 1", m.cursor)
	}
}

func TestTUIMouseSettingsUsesClickedDirection(t *testing.T) {
	m := Model{tab: settingsTab, width: 100, height: 24, settings: storage.DefaultSettings()}
	updated, _ := m.Update(tea.MouseMsg{X: 5, Y: 17, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(Model)
	if m.settingsCursor != 6 || m.settings.ColorTheme != "monochrome" {
		t.Fatalf("left settings click = cursor %d, theme %q", m.settingsCursor, m.settings.ColorTheme)
	}
}

func TestTUIMouseSettingsValueClickCyclesDirectionally(t *testing.T) {
	m := Model{tab: settingsTab, width: 100, height: 24, settings: storage.DefaultSettings(), settingsCursor: 2}
	m.settings.BarOrder = "percent_bar_reset"
	updated, _ := m.Update(tea.MouseMsg{X: 89, Y: 9, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(Model)
	if m.settings.BarOrder != "percent_reset_bar" {
		t.Fatalf("right value click = %q, want percent_reset_bar", m.settings.BarOrder)
	}
	updated, _ = m.Update(tea.MouseMsg{X: 87, Y: 9, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(Model)
	if m.settings.BarOrder != "percent_bar_reset" {
		t.Fatalf("left value click = %q, want percent_bar_reset", m.settings.BarOrder)
	}
}

func TestTUIMouseSelectsProvider(t *testing.T) {
	m := Model{width: 80, height: 24, authActive: true, authSelectingProvider: true}
	updated, command := m.Update(tea.MouseMsg{X: 6, Y: 11, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(Model)
	if m.authProviderID != providers.OpenCodeGo || m.authSelectingProvider || command != nil {
		t.Fatalf("provider click = %#v, command=%v", m, command)
	}
}

func TestTUISelectingClaudeStartsCodeLogin(t *testing.T) {
	m := Model{width: 102, height: 24, authActive: true, authSelectingProvider: true}
	updated, _ := m.Update(tea.MouseMsg{X: 6, Y: 10, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(Model)
	if m.authProviderID != providers.Claude || m.authSelectingProvider || m.authClaudeSession == nil {
		t.Fatalf("claude click = %#v", m)
	}
	for _, r := range "abc#state" {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(Model)
	}
	if m.authCodeInput != "abc#state" {
		t.Fatalf("code input = %q", m.authCodeInput)
	}
	view := m.View()
	if !strings.Contains(view, "Paste the code") || !strings.Contains(view, "claude.com/cai/oauth") {
		t.Fatalf("view = %s", view)
	}
	updated, command := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if !m.authLoading || command == nil {
		t.Fatalf("enter did not start sign in: %#v", m)
	}
}

func TestTUIMouseWheelNavigatesProviderPicker(t *testing.T) {
	m := Model{authActive: true, authSelectingProvider: true}
	updated, _ := m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	m = updated.(Model)
	if m.authProviderID != providers.Codex {
		t.Fatalf("provider after wheel = %q", m.authProviderID)
	}
}

func TestTUIMouseSelectsResetCredit(t *testing.T) {
	m := Model{
		tab: resetsTab, width: 100, height: 24, resetRowsFocused: false,
		accounts:       []storage.Account{{ID: "one", Name: "One", Provider: providers.Codex}},
		resetAccountID: "one", resetPayload: &codexapi.ResetCreditsPayload{Credits: []codexapi.ResetCreditDetail{{Title: "One"}, {Title: "Two"}}},
	}
	updated, _ := m.Update(tea.MouseMsg{X: 50, Y: 13, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(Model)
	if !m.resetRowsFocused || m.creditCursor != 1 {
		t.Fatalf("reset click = focused %v, cursor %d", m.resetRowsFocused, m.creditCursor)
	}
}

func TestTUIMouseSecondClickArmsResetCredit(t *testing.T) {
	credit := codexapi.ResetCreditDetail{Status: "available", Title: "One", GrantedAt: "2026-08-01T00:00:00Z"}
	m := Model{
		tab: resetsTab, width: 100, height: 24, resetRowsFocused: true,
		accounts:       []storage.Account{{ID: "one", Name: "One", Provider: providers.Codex}},
		resetAccountID: "one", resetPayload: &codexapi.ResetCreditsPayload{Credits: []codexapi.ResetCreditDetail{credit}},
	}
	m.lastMouseTarget = "reset:" + resetCreditKey(credit)
	m.lastMouseAt = time.Now()
	updated, _ := m.Update(tea.MouseMsg{X: 50, Y: 10, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(Model)
	if m.consumeArmed != resetCreditKey(credit) {
		t.Fatalf("reset click did not arm credit: %#v", m)
	}
}

func TestTUIMouseDoubleClickReauthenticatesAccount(t *testing.T) {
	account := storage.Account{ID: "one", Name: "One", Provider: providers.DeepSeek}
	m := Model{tab: usageTab, width: 100, height: 24, accounts: []storage.Account{account}, rows: []usage.Row{{ID: "one", Name: "One", ProviderID: providers.DeepSeek}}, lastMouseTarget: "account:one", lastMouseAt: time.Now()}
	updated, _ := m.Update(tea.MouseMsg{X: 3, Y: 7, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(Model)
	if !m.authActive || m.authProviderID != providers.DeepSeek || m.authReauthID != "one" {
		t.Fatalf("double click did not reauthenticate: %#v", m)
	}
}

func TestTUIProviderMetricsKeepResetsSeparate(t *testing.T) {
	used := 35.0
	reset := time.Now().Add(2 * time.Hour).Unix()
	m := Model{width: 110, height: 24, settings: storage.DefaultSettings(), rows: []usage.Row{{
		Name: "OpenCode", ProviderID: providers.OpenCodeGo, Provider: "OpenCode", Plan: "Go",
		Metrics: []providers.Metric{{Kind: providers.Percentage, Slot: providers.SessionSlot, Label: "SESSION", Used: &used, ResetAt: &reset}, {Kind: providers.Percentage, Slot: providers.WeeklySlot, Label: "WEEKLY", Used: &used}, {Kind: providers.Percentage, Slot: providers.MonthlySlot, Label: "MONTHLY", Used: &used}},
	}}}
	view := ansi.Strip(m.renderUsageTab(106, 22))
	for _, expected := range []string{"SESSION", "WEEKLY", "MONTHLY", "35%", "RESETS", "-"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("usage view missing %q:\n%s", expected, view)
		}
	}
	if strings.Contains(view, "2026-") || strings.Contains(view, "Resets   ") {
		t.Fatalf("OpenCode view contains raw timestamp or reset credits:\n%s", view)
	}
}

func TestTUIDeepSeekBalanceIsVisible(t *testing.T) {
	m := Model{width: 110, height: 24, settings: storage.DefaultSettings(), rows: []usage.Row{{
		Name: "DeepSeek", ProviderID: providers.DeepSeek, Provider: "DeepSeek", Plan: "USD 12.50",
	}}}
	view := ansi.Strip(m.renderUsageTab(106, 22))
	if !strings.Contains(view, "USD 12.50") || !strings.Contains(view, "SESSION") || !strings.Contains(view, "WEEKLY") || !strings.Contains(view, "-") {
		t.Fatalf("DeepSeek balance is missing:\n%s", view)
	}
	if strings.Contains(view, "BALANCE") || strings.Contains(view, "available") {
		t.Fatalf("DeepSeek balance leaked into usage metrics:\n%s", view)
	}
}

func TestTUIMouseLoadsResetAccountWithoutCache(t *testing.T) {
	m := Model{
		tab: resetsTab, width: 100, height: 24,
		accounts:    []storage.Account{{ID: "one", Name: "One", Provider: providers.Codex}},
		resetCache:  map[string]*codexapi.ResetCreditsPayload{},
		resetLoader: func(string) (*codexapi.ResetCreditsPayload, error) { return &codexapi.ResetCreditsPayload{}, nil },
	}
	updated, command := m.Update(tea.MouseMsg{X: 3, Y: 7, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(Model)
	if command == nil || !m.resetLoading || m.resetAccountID != "one" {
		t.Fatalf("reset account click = loading %v, account %q, command %v", m.resetLoading, m.resetAccountID, command)
	}
}

func TestResetsTabHidesUnsupportedProviders(t *testing.T) {
	m := Model{accounts: []storage.Account{{ID: "codex", Provider: providers.Codex}, {ID: "deepseek", Provider: providers.DeepSeek}}}
	accounts := m.resetAccounts()
	if len(accounts) != 1 || accounts[0].ID != "codex" {
		t.Fatalf("reset accounts = %#v", accounts)
	}
}

func TestResetSidebarCompactModeUsesSingleLineAccounts(t *testing.T) {
	m := Model{
		accounts: []storage.Account{{Name: "Personal", Provider: providers.Codex}},
		settings: storage.Settings{CompactMode: true},
	}
	sidebar := ansi.Strip(m.renderResetSidebar(30, 20))
	if !strings.Contains(sidebar, "Personal · Codex") {
		t.Fatalf("compact reset sidebar did not use one line:\n%s", sidebar)
	}
}

func TestTUISettingsSaveHasNoSuccessToast(t *testing.T) {
	m := Model{tab: settingsTab, notice: "old notice"}
	updated, _ := m.Update(storeSavedMsg{action: "Settings saved"})
	m = updated.(Model)
	if m.notice != "" {
		t.Fatalf("settings save notice = %q, want empty", m.notice)
	}
}

func TestTUIAutoRefreshIgnoresOldTimer(t *testing.T) {
	m := Model{timerVersion: 3, initialized: true}
	updated, cmd := m.Update(autoRefreshTickMsg{version: 2})
	m = updated.(Model)
	if m.loading || cmd != nil {
		t.Fatal("stale auto-refresh timer started a refresh")
	}

	updated, cmd = m.Update(autoRefreshTickMsg{version: 3})
	m = updated.(Model)
	if !m.loading || cmd == nil {
		t.Fatal("current auto-refresh timer did not start a refresh")
	}
}

func TestResetConsumptionRequiresSecondConfirmationWithoutMutation(t *testing.T) {
	credit := codexapi.ResetCreditDetail{Status: "available", Title: "Full reset", GrantedAt: "g", ExpiresAt: "e"}
	m := Model{tab: resetsTab, resetRowsFocused: true, resetPayload: &codexapi.ResetCreditsPayload{Credits: []codexapi.ResetCreditDetail{credit}}}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.consumeArmed == "" || !strings.Contains(m.notice, "again") {
		t.Fatalf("first consume action did not arm confirmation: %#v", m)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.consumeArmed != "" || !strings.Contains(m.notice, "not connected yet") {
		t.Fatalf("second consume action did not stop at placeholder: %#v", m)
	}
	if m.resetPayload.Credits[0].Status != "available" {
		t.Fatal("placeholder consumption mutated the reset credit")
	}
}

func TestTUIHelpDoesNotAdvertiseScriptCommands(t *testing.T) {
	m := Model{showHelp: true, width: 110, height: 24}
	view := m.View()
	if strings.Contains(view, "scriptable") || strings.Contains(view, "accounts login") {
		t.Fatalf("help still contains script command information:\n%s", view)
	}
}

func TestFocusedTabFooterDescribesFocusControls(t *testing.T) {
	m := Model{tabRowFocused: true}
	footer := ansi.Strip(m.renderFooter(100))
	for _, want := range []string{"←/→ switch tab", "↑/↓ resume + move", "tab resume content"} {
		if !strings.Contains(footer, want) {
			t.Fatalf("focused tab footer missing %q: %s", want, footer)
		}
	}
	if strings.Contains(footer, "↓ enter content") {
		t.Fatalf("focused tab footer contains stale binding: %s", footer)
	}
}

func TestEveryTabFooterShowsQuitBinding(t *testing.T) {
	for _, tab := range []tuiTab{usageTab, resetsTab, settingsTab} {
		m := Model{tab: tab}
		footer := ansi.Strip(m.renderFooter(44))
		if !strings.Contains(footer, "q quit") {
			t.Fatalf("tab %d footer hides quit binding: %s", tab, footer)
		}
	}

	m := Model{tab: resetsTab, resetRowsFocused: true}
	if footer := ansi.Strip(m.renderFooter(44)); !strings.Contains(footer, "q quit") {
		t.Fatalf("focused reset footer hides quit binding: %s", footer)
	}
}

func TestUsageTabSupportsReauthentication(t *testing.T) {
	m := Model{tab: usageTab, accounts: []storage.Account{{ID: "one", Name: "Personal", Provider: providers.Codex}}, rows: []usage.Row{{ID: "one", Name: "Personal", ProviderID: providers.Codex}}}
	if footer := ansi.Strip(m.renderFooter(100)); !strings.Contains(footer, "x reauth") {
		t.Fatalf("usage footer does not document reauthentication: %s", footer)
	}
	updated, command := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(Model)
	if command == nil || !m.authActive || !m.authLoading || m.authReauthID != "one" {
		t.Fatal("x did not start reauthentication")
	}
	if m.tab != usageTab {
		t.Fatalf("tab changed to %d", m.tab)
	}
}

func TestTUIAuthenticationRendersDeviceCodeAndCancels(t *testing.T) {
	m := Model{width: 110, height: 24, authActive: true, authLoading: true, authVersion: 1}
	updated, command := m.Update(authCodeLoadedMsg{code: &codexapi.DeviceUserCodeResponse{UserCode: "ABCD-EFGH"}, version: 1})
	m = updated.(Model)
	if command == nil || !m.authLoading || m.authCode == nil {
		t.Fatalf("authorization code state = %#v", m)
	}
	view := ansi.Strip(m.View())
	for _, want := range []string{"Add account", codexapi.DeviceVerificationURL, "ABCD-EFGH", "Waiting for approval", "ctrl+y copy link   esc cancel"} {
		if !strings.Contains(view, want) {
			t.Fatalf("authentication view missing %q:\n%s", want, view)
		}
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.authActive || m.authVersion != 2 {
		t.Fatalf("authentication was not cancelled: %#v", m)
	}
}

func TestTUIAPIKeyInputAcceptsPastedRunes(t *testing.T) {
	m := Model{authActive: true, authProviderID: providers.DeepSeek}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("sk-pasted-key")})
	m = updated.(Model)
	if m.authAPIKeyInput != "sk-pasted-key" {
		t.Fatalf("API key input = %q", m.authAPIKeyInput)
	}
}

func TestTUISizeHintRendersWhenTerminalTooSmall(t *testing.T) {
	for _, width := range []int{20, 60, 101} {
		m := Model{width: width, height: 11, settings: storage.DefaultSettings(), rows: []usage.Row{{Name: "One"}}}
		view := m.View()
		if !strings.Contains(view, "Terminal too") || !strings.Contains(view, "102×12") {
			t.Fatalf("width %d, height 11: missing size hint:\n%s", width, view)
		}
		for _, line := range strings.Split(view, "\n") {
			if got := lipgloss.Width(line); got > width {
				t.Fatalf("width %d: hint line width = %d: %q", width, got, line)
			}
		}
	}

	m := Model{width: tuiMinWidth, height: tuiMinHeight, settings: storage.DefaultSettings()}
	view := m.View()
	if strings.Contains(view, "Terminal too small") {
		t.Fatalf("minimum terminal size still shows the size hint:\n%s", view)
	}
}

func TestTUIViewHasOneRowShellMargin(t *testing.T) {
	m := Model{width: 110, height: 24, settings: storage.DefaultSettings()}
	view := m.View()
	if !strings.HasPrefix(view, "\n") || !strings.HasSuffix(view, "\n") {
		t.Fatalf("View() does not have a one-row vertical shell margin: %q", view)
	}
}

func TestTUIViewFitsCommonTerminalWidths(t *testing.T) {
	used := 99.0
	for _, width := range []int{20, 24, 32, 44, 64, 100} {
		m := Model{
			width: width, height: 24,
			settings: storage.DefaultSettings(),
			accounts: []storage.Account{{
				ID: "one", Name: "An account with an exceptionally long display name", Provider: providers.Codex,
				Email: new("a-very-long-address@example.com"), PlanType: new("enterprise"),
			}},
			rows: []usage.Row{{
				Name:  "An account with an exceptionally long display name",
				Email: "a-very-long-address@example.com", ProviderID: providers.Codex, Provider: "Codex", Plan: "enterprise",
				Metrics:      []providers.Metric{{Kind: providers.Percentage, Slot: providers.SessionSlot, Label: "SESSION", Used: &used}, {Kind: providers.Percentage, Slot: providers.WeeklySlot, Label: "WEEKLY", Used: &used}},
				ResetCredits: "12, earliest exp. in 24h", SupportsResetCredits: true,
			}},
		}
		m.resetPayload = &codexapi.ResetCreditsPayload{AvailableCount: 1, TotalEarnedCount: 1, Credits: []codexapi.ResetCreditDetail{{
			Status: "available", Title: "A reset credit with a very long descriptive title",
			GrantedAt: "2026-08-01T12:00:00Z", ExpiresAt: "2026-08-09T12:00:00Z",
		}}}
		for _, tab := range []tuiTab{usageTab, resetsTab, settingsTab} {
			m.tab = tab
			view := m.View()
			if got := lipgloss.Height(view); got > m.height {
				t.Fatalf("View() tab %d height = %d, terminal height = %d", tab, got, m.height)
			}
			for _, line := range strings.Split(view, "\n") {
				if got := lipgloss.Width(line); got > width {
					t.Fatalf("View() tab %d line width = %d, terminal width = %d:\n%q", tab, got, width, line)
				}
			}
		}
	}
}

func TestTUIViewRendersDashboardAndCompactLayout(t *testing.T) {
	primary := 42.0
	secondary := 73.0
	m := Model{
		rows: []usage.Row{{
			Name: "Personal", Email: "person@example.com", ProviderID: providers.Codex, Provider: "Codex", Plan: "plus",
			Metrics:      []providers.Metric{{Kind: providers.Percentage, Slot: providers.SessionSlot, Label: "SESSION", Used: &primary}, {Kind: providers.Percentage, Slot: providers.WeeklySlot, Label: "WEEKLY", Used: &secondary}},
			ResetCredits: "2, earliest exp. in 1d", SupportsResetCredits: true,
		}},
		width: 110, height: 24,
	}

	view := m.View()
	for _, want := range []string{"AI", "USAGE", "Personal", "Codex", "SESSION", "WEEKLY", "MONTHLY", "42%", "73%"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() missing %q:\n%s", want, view)
		}
	}
}

func TestNonCompactListShowsBarResetTimes(t *testing.T) {
	used := 25.0
	resetAt := func(offset time.Duration) *int64 {
		v := time.Now().Add(offset).Unix()
		return &v
	}
	row := usage.Row{
		Name: "Personal", Email: "person@example.com", ProviderID: providers.OpenCodeGo, Provider: "OpenCode Go", Plan: "pro",
		Metrics: []providers.Metric{
			{Kind: providers.Percentage, Slot: providers.SessionSlot, Label: "SESSION", Used: &used, ResetAt: resetAt(90 * time.Minute)},
			{Kind: providers.Percentage, Slot: providers.WeeklySlot, Label: "WEEKLY", Used: &used, ResetAt: resetAt(3 * 24 * time.Hour)},
			{Kind: providers.Percentage, Slot: providers.MonthlySlot, Label: "MONTHLY", Used: &used, ResetAt: resetAt(31 * 24 * time.Hour)},
		},
	}
	for _, wide := range []bool{true, false} {
		now := time.Now()
		sessionDate := now.Add(90 * time.Minute).Format("Jan 2 15:04")
		weeklyDate := now.Add(3 * 24 * time.Hour).Format("Jan 2 15:04")
		nonCompact := Model{rows: []usage.Row{row}, settings: storage.Settings{CompactMode: false}, width: 110, height: 24}
		var view string
		if wide {
			view = ansi.Strip(nonCompact.renderWideList(110, 22))
		} else {
			view = ansi.Strip(nonCompact.renderCompactList(110, 22))
		}
		for _, want := range []string{"·", "1h29m", "2d23h", "30d23h", sessionDate, weeklyDate} {
			if !strings.Contains(view, want) {
				t.Fatalf("wide=%v non-compact list missing %q:\n%s", wide, want, view)
			}
		}
		compact := Model{rows: []usage.Row{row}, settings: storage.Settings{CompactMode: true}, width: 110, height: 24}
		if wide {
			view = ansi.Strip(compact.renderWideList(110, 22))
		} else {
			view = ansi.Strip(compact.renderCompactList(110, 22))
		}
		if strings.Contains(view, sessionDate) {
			t.Fatalf("wide=%v compact list still shows reset dates:\n%s", wide, view)
		}
	}
}

func TestWideListCompactModeShowsResetCountdownInBars(t *testing.T) {
	used := 25.0
	resetAt := time.Now().Add(7*24*time.Hour + 3*time.Hour).Unix()
	row := usage.Row{
		Name: "Personal", Email: "person@example.com", ProviderID: providers.OpenCodeGo, Provider: "OpenCode Go", Plan: "pro",
		Metrics: []providers.Metric{
			{Kind: providers.Percentage, Slot: providers.SessionSlot, Label: "SESSION", Used: &used, ResetAt: &resetAt},
			{Kind: providers.Percentage, Slot: providers.WeeklySlot, Label: "WEEKLY", Used: &used, ResetAt: &resetAt},
			{Kind: providers.Percentage, Slot: providers.MonthlySlot, Label: "MONTHLY", Used: &used, ResetAt: &resetAt},
		},
	}
	compact := Model{rows: []usage.Row{row}, settings: storage.Settings{CompactMode: true, BarOrder: "bar_percent_reset"}, width: 110, height: 24}
	view := ansi.Strip(compact.renderWideList(110, 22))
	if !strings.Contains(view, "7d2h") || strings.Contains(view, "·") {
		t.Fatalf("compact wide list lost the bar reset countdown:\n%s", view)
	}
	spaced := Model{rows: []usage.Row{row}, settings: storage.Settings{CompactMode: false, BarOrder: "bar_percent_reset"}, width: 110, height: 24}
	view = ansi.Strip(spaced.renderWideList(110, 22))
	expectedDate := time.Unix(resetAt, 0).Format("Jan 2 15:04")
	if !strings.Contains(view, "7d2h") || !strings.Contains(view, "·") || !strings.Contains(view, expectedDate) {
		t.Fatalf("spaced wide list lost countdown or reset date (%q):\n%s", expectedDate, view)
	}
}

func TestVisibleRangeKeepsCursorOnScreen(t *testing.T) {
	start, end := visibleRange(20, 18, 5)
	if start != 15 || end != 20 {
		t.Fatalf("visibleRange() = (%d, %d), want (15, 20)", start, end)
	}
}

func TestTUIMarksModelScopedLimitInMonthlyColumn(t *testing.T) {
	used := 40.0
	reset := time.Now().Add(48 * time.Hour).Unix()
	row := usage.Row{Name: "Claude", ProviderID: providers.Claude, Provider: "Claude", Metrics: []providers.Metric{
		{Kind: providers.Percentage, Slot: providers.MonthlySlot, Label: "FABLE", Scope: "Fable weekly", Used: &used, ResetAt: &reset},
	}}
	m := Model{width: 120, height: 24, initialized: true, rows: []usage.Row{row}, settings: storage.DefaultSettings()}
	view := m.View()
	if !strings.Contains(view, "MONTHLY ✦") || !strings.Contains(view, "✦ ━") || !strings.Contains(view, "Fable weekly · ") {
		t.Fatalf("view = %s", view)
	}
}

func TestTUICodeLoginCopiesLinkAndKeepsLayoutWidth(t *testing.T) {
	m := Model{width: 102, height: 24, authActive: true, authSelectingProvider: true}
	updated, _ := m.Update(tea.MouseMsg{X: 6, Y: 10, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(Model)
	if _, command := m.Update(tea.KeyMsg{Type: tea.KeyCtrlY}); command == nil {
		t.Fatal("ctrl+y did not copy the link")
	}
	linkY, linkX := -1, 0
	for y, line := range strings.Split(m.View(), "\n") {
		if strings.Contains(line, "\x1b]8;;") {
			plain := ansi.Strip(line)
			linkY, linkX = y, len(plain)-len(strings.TrimLeft(plain, " "))
			break
		}
	}
	if linkY < 0 {
		t.Fatal("link not rendered")
	}
	if _, command := m.Update(tea.MouseMsg{X: linkX, Y: linkY, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}); command == nil {
		t.Fatal("click on the link did not copy it")
	}
	for _, miss := range []tea.MouseMsg{{X: linkX, Y: 3}, {X: linkX, Y: linkY - 3}, {X: max(0, linkX-1), Y: linkY}, {X: m.width - 1, Y: linkY + 10}} {
		miss.Action, miss.Button = tea.MouseActionPress, tea.MouseButtonLeft
		if _, command := m.Update(miss); command != nil {
			t.Fatalf("click at %d,%d copied the link", miss.X, miss.Y)
		}
	}
	updated, _ = m.Update(linkCopiedMsg{native: true, version: m.authVersion})
	m = updated.(Model)
	view := m.View()
	if !strings.Contains(view, "Link copied to clipboard") || !strings.Contains(view, "\x1b]8;;"+m.authClaudeSession.URL) {
		t.Fatalf("view = %q", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if lipgloss.Width(line) > m.width {
			t.Fatalf("line wider than terminal (%d): %q", lipgloss.Width(line), line)
		}
	}
}

func TestTUIBrowserFlowsShareLinkPresentation(t *testing.T) {
	codex := Model{width: 110, height: 24, authActive: true, authLoading: true, authProviderID: providers.Codex, authVersion: 1}
	updated, _ := codex.Update(authCodeLoadedMsg{code: &codexapi.DeviceUserCodeResponse{UserCode: "ABCD-EFGH"}, version: 1})
	codex = updated.(Model)
	start := func(id string) Model {
		m := Model{width: 110, height: 24, authActive: true, authSelectingProvider: true, authProviderID: id}
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		return updated.(Model)
	}
	for name, m := range map[string]Model{"codex": codex, "claude": start(providers.Claude), "cursor": start(providers.Cursor)} {
		if m.authLink == "" {
			t.Fatalf("%s: no sign-in link", name)
		}
		view := m.View()
		plain := ansi.Strip(view)
		if !strings.Contains(view, "\x1b]8;;"+m.authLink) || !strings.Contains(plain, "in the browser window that opened") ||
			!strings.Contains(plain, "click the link below") || !strings.Contains(plain, "ctrl+y copy link") {
			t.Fatalf("%s: inconsistent link presentation:\n%s", name, plain)
		}
		if _, command := m.Update(tea.KeyMsg{Type: tea.KeyCtrlY}); command == nil {
			t.Fatalf("%s: ctrl+y did not copy the link", name)
		}
	}
}

func TestTUIAuthDialogsAnimateFromTheStart(t *testing.T) {
	if _, command := (Model{width: 102, height: 24}).startAccountAdd(); command == nil {
		t.Fatal("add account did not start the spinner")
	}
	account := storage.Account{ID: "one", Name: "One", Provider: providers.Codex}
	if _, command := (Model{width: 102, height: 24, accounts: []storage.Account{account}}).startAccountReauthentication(account); command == nil {
		t.Fatal("reauthentication did not start the spinner")
	}
	m := Model{authActive: true}
	updated, command := m.Update(spinnerTickMsg{})
	if step := updated.(Model).spinnerStep; command == nil || spinnerStepAt(time.Now())-step > 1 || step <= 0 {
		t.Fatal("spinner frame is not derived from the clock")
	}
}

func TestTUIShowsEarliestResetCreditExpiry(t *testing.T) {
	exp := time.Now().Add(26*time.Hour + 5*time.Minute).Unix()
	row := usage.Row{Name: "Codex", ProviderID: providers.Codex, Provider: "Codex", SupportsResetCredits: true, ResetCredits: "2, exp. stale text", ResetCreditsExpireAt: &exp}
	for _, compact := range []bool{false, true} {
		settings := storage.DefaultSettings()
		settings.CompactMode = compact
		m := Model{width: 120, height: 24, rows: []usage.Row{row}, settings: settings}
		lines := strings.Split(ansi.Strip(m.renderWideList(tuiContentWidth(120), 24)), "\n")
		var countLine, nextLine string
		for index, line := range lines {
			if strings.Contains(line, "Codex") {
				countLine, nextLine = line, lines[min(index+1, len(lines)-1)]
				break
			}
		}
		if compact && !strings.HasSuffix(strings.TrimSpace(countLine), "2 1d2h") {
			t.Fatalf("compact row = %q", countLine)
		}
		if !compact && (!strings.HasSuffix(strings.TrimSpace(nextLine), "1d2h") || ansi.StringWidth(nextLine[:strings.Index(nextLine, "1d2h")]) != ansi.StringWidth(countLine[:strings.LastIndex(countLine, "2")])) {
			t.Fatalf("expiry not under the count:\n%q\n%q", countLine, nextLine)
		}
		if strings.Contains(strings.Join(lines, "\n"), "stale text") {
			t.Fatal("rendered the fetch-time summary instead of a live countdown")
		}
	}
	if got := usage.CreditExpirySubtitle(row, time.Now(), 40); !strings.HasPrefix(got, "1d2h · ") {
		t.Fatalf("wide subtitle = %q", got)
	}
}

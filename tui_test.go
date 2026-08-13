package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestTUIKeyboardNavigation(t *testing.T) {
	m := tuiModel{rows: []usageRow{{Name: "One"}, {Name: "Two"}, {Name: "Three"}}}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(tuiModel)
	if m.cursor != 1 {
		t.Fatalf("down cursor = %d, want 1", m.cursor)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	m = updated.(tuiModel)
	if m.cursor != 2 {
		t.Fatalf("G cursor = %d, want 2", m.cursor)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(tuiModel)
	if m.cursor != 2 {
		t.Fatalf("cursor moved past last row: %d", m.cursor)
	}
}

func TestTUILeftAndRightSwitchTabs(t *testing.T) {
	m := tuiModel{tab: usageTab}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(tuiModel)
	if m.tab != usageTab {
		t.Fatalf("unfocused right selected tab %d, want usage", m.tab)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(tuiModel)
	if !m.tabRowFocused {
		t.Fatal("Tab did not focus tabs")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(tuiModel)
	if m.tab != resetsTab {
		t.Fatalf("right selected tab %d, want resets", m.tab)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = updated.(tuiModel)
	if m.tab != usageTab {
		t.Fatalf("left selected tab %d, want usage", m.tab)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(tuiModel)
	if m.tabRowFocused {
		t.Fatal("down did not return focus to tab content")
	}
}

func TestTUIUpDoesNotFocusTabsAndTabPreservesSelection(t *testing.T) {
	m := tuiModel{tab: usageTab, cursor: 2, rows: []usageRow{{}, {}, {}}}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(tuiModel)
	if m.tabRowFocused || m.cursor != 1 {
		t.Fatalf("Up result: focused=%v cursor=%d", m.tabRowFocused, m.cursor)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(tuiModel)
	if !m.tabRowFocused || m.cursor != 1 {
		t.Fatalf("Tab changed selection: focused=%v cursor=%d", m.tabRowFocused, m.cursor)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(tuiModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = updated.(tuiModel)
	if m.cursor != 1 {
		t.Fatalf("returning to Usage restored cursor %d, want 1", m.cursor)
	}
}

func TestTUITabFocusesTabRowWithoutSwitching(t *testing.T) {
	m := tuiModel{tab: accountsTab}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(tuiModel)
	if !m.tabRowFocused || m.tab != accountsTab {
		t.Fatalf("Tab result: focused=%v tab=%d", m.tabRowFocused, m.tab)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(tuiModel)
	if m.tabRowFocused {
		t.Fatal("second Tab did not restore content focus")
	}
}

func TestTUITabFocusUpAndDownReturnToContentAndMove(t *testing.T) {
	m := tuiModel{tab: usageTab, tabRowFocused: true, cursor: 1, rows: []usageRow{{}, {}, {}}}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(tuiModel)
	if m.tabRowFocused || m.cursor != 0 {
		t.Fatalf("focused Up result: focused=%v cursor=%d", m.tabRowFocused, m.cursor)
	}

	m.tabRowFocused = true
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(tuiModel)
	if m.tabRowFocused || m.cursor != 1 {
		t.Fatalf("focused Down result: focused=%v cursor=%d", m.tabRowFocused, m.cursor)
	}
}

func TestTUIResetSidebarEnterSelectsAccountAndFocusesRows(t *testing.T) {
	cached := &resetCreditsPayload{AvailableCount: 2}
	m := tuiModel{
		tab:            resetsTab,
		accounts:       []storedAccount{{ID: "one", Name: "One", Provider: providerOpenAICodex}, {ID: "two", Name: "Two", Provider: providerOpenAICodex}},
		resetAccountID: "one",
		resetPayload:   &resetCreditsPayload{},
		resetCache:     map[string]*resetCreditsPayload{"two": cached},
		resetLoader:    func(string) (*resetCreditsPayload, error) { return &resetCreditsPayload{}, nil },
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(tuiModel)
	if m.cursor != 1 || m.resetRowsFocused || m.resetAccountID != "two" || m.resetPayload != cached {
		t.Fatalf("sidebar navigation: cursor=%d rowsFocused=%v account=%q payload=%p", m.cursor, m.resetRowsFocused, m.resetAccountID, m.resetPayload)
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(tuiModel)
	if !m.resetRowsFocused || m.resetAccountID != "two" || cmd == nil {
		t.Fatalf("account selection: rowsFocused=%v account=%q cmd=%v", m.resetRowsFocused, m.resetAccountID, cmd)
	}
}

func TestTUIResetSidebarDoesNotRenderSelectedLabel(t *testing.T) {
	m := tuiModel{
		tab: resetsTab, width: 80, height: 24,
		accounts:       []storedAccount{{ID: "one", Name: "One", Provider: providerOpenAICodex}},
		resetAccountID: "one",
		resetPayload:   &resetCreditsPayload{},
	}
	if view := m.View(); strings.Contains(view, "selected") {
		t.Fatalf("reset sidebar still renders selected label:\n%s", view)
	}
}

func TestTUIUsageLoadedPreservesRowsOnRefreshError(t *testing.T) {
	m := tuiModel{rows: []usageRow{{Name: "Existing"}}, loading: true, initialized: true}
	updated, _ := m.Update(dashboardLoadedMsg{err: errors.New("offline"), at: time.Now()})
	m = updated.(tuiModel)

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
	m := tuiModel{
		tab:      settingsTab,
		settings: defaultAppSettings(),
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(tuiModel)
	if m.settings.UsageDisplay != "remaining" {
		t.Fatalf("usage display = %q, want remaining", m.settings.UsageDisplay)
	}

	m.settingsCursor = 1
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(tuiModel)
	if m.settings.BarFill != "right" {
		t.Fatalf("bar fill = %q, want right", m.settings.BarFill)
	}

	m.settingsCursor = 2
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(tuiModel)
	if m.settings.PercentagePosition != "left" {
		t.Fatalf("percentage position = %q, want left", m.settings.PercentagePosition)
	}

	m.settingsCursor = 3
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(tuiModel)
	if m.settings.ColorTheme != "colorblind" {
		t.Fatalf("color theme = %q, want colorblind", m.settings.ColorTheme)
	}

	m.settingsCursor = 4
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(tuiModel)
	if m.settings.AutoRefreshSeconds != 300 {
		t.Fatalf("auto refresh = %d, want 300", m.settings.AutoRefreshSeconds)
	}

	m.settingsCursor = 5
	m.settings.CompactMode = false
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(tuiModel)
	if !m.settings.CompactMode {
		t.Fatal("compact mode remained disabled")
	}
}

func TestUsageBarFillAndPercentagePlacementAreIndependent(t *testing.T) {
	used := 40.0
	leftFillRightPercentage := ansi.Strip(renderUsageBar(&used, 20, false, false, false, false, "left", "right", "default"))
	rightFillRightPercentage := ansi.Strip(renderUsageBar(&used, 20, false, false, false, false, "right", "right", "default"))
	leftFillLeftPercentage := ansi.Strip(renderUsageBar(&used, 20, false, false, false, false, "left", "left", "default"))
	rightFillLeftPercentage := ansi.Strip(renderUsageBar(&used, 20, false, false, false, false, "right", "left", "default"))
	if !strings.HasPrefix(leftFillRightPercentage, "━") || !strings.HasSuffix(leftFillRightPercentage, " 40%") {
		t.Fatalf("left fill/right percentage bar = %q", leftFillRightPercentage)
	}
	if !strings.HasPrefix(rightFillRightPercentage, "─") || !strings.HasSuffix(rightFillRightPercentage, " 40%") {
		t.Fatalf("right fill/right percentage bar = %q", rightFillRightPercentage)
	}
	if !strings.HasPrefix(leftFillLeftPercentage, "40% ") || !strings.HasSuffix(leftFillLeftPercentage, "─") {
		t.Fatalf("left fill/left percentage bar = %q", leftFillLeftPercentage)
	}
	if !strings.HasPrefix(rightFillLeftPercentage, "40% ") || !strings.HasSuffix(rightFillLeftPercentage, "━") {
		t.Fatalf("right fill/left percentage bar = %q", rightFillLeftPercentage)
	}
}

func TestUsageListLabelsStaleFallbackRows(t *testing.T) {
	used := 25.0
	m := tuiModel{
		width:    110,
		height:   24,
		settings: defaultAppSettings(),
		rows:     []usageRow{{Name: "Personal", ProviderID: providerOpenAICodex, Provider: "OpenAI Codex", Plan: "free", Metrics: []providerMetric{{Kind: percentageMetric, Slot: sessionSlot, Label: "SESSION", Used: &used}, {Kind: percentageMetric, Slot: weeklySlot, Label: "WEEKLY", Used: &used}}, Stale: true}},
	}
	if view := ansi.Strip(m.renderUsageTab(106, 22)); !strings.Contains(view, "(Stale)") {
		t.Fatalf("stale usage row is not labeled:\n%s", view)
	}
}

func TestUsageListShowsAuthenticationRecovery(t *testing.T) {
	m := tuiModel{
		width: 110, height: 24, settings: defaultAppSettings(),
		rows: []usageRow{{Name: "Personal", ProviderID: providerOpenAICodex, Provider: "OpenAI Codex", Plan: "free", AuthRequired: true}},
	}
	view := ansi.Strip(m.renderUsageTab(106, 22))
	if !strings.Contains(strings.ToLower(view), "sign in required") {
		t.Fatalf("authentication status is missing:\n%s", view)
	}
	if !strings.Contains(view, "Open Accounts and press r to reconnect.") {
		t.Fatalf("authentication recovery instruction missing:\n%s", view)
	}
}

func TestTUIMouseClickChangesTabAndSelectsUsageRow(t *testing.T) {
	m := tuiModel{width: 100, height: 24, settings: defaultAppSettings(), rows: []usageRow{{Name: "One"}, {Name: "Two"}}}
	updated, _ := m.Update(tea.MouseMsg{X: 12, Y: 2, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(tuiModel)
	if m.tab != resetsTab {
		t.Fatalf("mouse tab = %d, want resets", m.tab)
	}
	m.tab = usageTab
	updated, _ = m.Update(tea.MouseMsg{X: 4, Y: 9, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(tuiModel)
	if m.cursor != 1 {
		t.Fatalf("mouse row = %d, want 1", m.cursor)
	}
}

func TestTUIMouseSettingsUsesClickedDirection(t *testing.T) {
	m := tuiModel{tab: settingsTab, width: 100, height: 24, settings: defaultAppSettings()}
	updated, _ := m.Update(tea.MouseMsg{X: 5, Y: 11, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(tuiModel)
	if m.settingsCursor != 3 || m.settings.ColorTheme != "monochrome" {
		t.Fatalf("left settings click = cursor %d, theme %q", m.settingsCursor, m.settings.ColorTheme)
	}
}

func TestTUIMouseSelectsProvider(t *testing.T) {
	m := tuiModel{width: 80, height: 24, authActive: true, authSelectingProvider: true}
	updated, command := m.Update(tea.MouseMsg{X: 6, Y: 10, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(tuiModel)
	if m.authProviderID != providerOpenCodeGo || m.authSelectingProvider || command != nil {
		t.Fatalf("provider click = %#v, command=%v", m, command)
	}
}

func TestTUIMouseWheelNavigatesProviderPicker(t *testing.T) {
	m := tuiModel{authActive: true, authSelectingProvider: true}
	updated, _ := m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	m = updated.(tuiModel)
	if m.authProviderID != providerOpenAICodex {
		t.Fatalf("provider after wheel = %q", m.authProviderID)
	}
}

func TestTUIMouseSelectsResetCredit(t *testing.T) {
	m := tuiModel{
		tab: resetsTab, width: 100, height: 24, resetRowsFocused: false,
		accounts:       []storedAccount{{ID: "one", Name: "One", Provider: providerOpenAICodex}},
		resetAccountID: "one", resetPayload: &resetCreditsPayload{Credits: []resetCreditDetail{{Title: "One"}, {Title: "Two"}}},
	}
	updated, _ := m.Update(tea.MouseMsg{X: 50, Y: 13, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(tuiModel)
	if !m.resetRowsFocused || m.creditCursor != 1 {
		t.Fatalf("reset click = focused %v, cursor %d", m.resetRowsFocused, m.creditCursor)
	}
}

func TestTUIMouseSecondClickArmsResetCredit(t *testing.T) {
	credit := resetCreditDetail{Status: "available", Title: "One", GrantedAt: "2026-08-01T00:00:00Z"}
	m := tuiModel{
		tab: resetsTab, width: 100, height: 24, resetRowsFocused: true,
		accounts:       []storedAccount{{ID: "one", Name: "One", Provider: providerOpenAICodex}},
		resetAccountID: "one", resetPayload: &resetCreditsPayload{Credits: []resetCreditDetail{credit}},
	}
	m.lastMouseTarget = "reset:" + resetCreditKey(credit)
	m.lastMouseAt = time.Now()
	updated, _ := m.Update(tea.MouseMsg{X: 50, Y: 10, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(tuiModel)
	if m.consumeArmed != resetCreditKey(credit) {
		t.Fatalf("reset click did not arm credit: %#v", m)
	}
}

func TestTUIMouseDoubleClickReauthenticatesAccount(t *testing.T) {
	account := storedAccount{ID: "one", Name: "One", Provider: providerDeepSeek}
	m := tuiModel{tab: accountsTab, width: 100, height: 24, accounts: []storedAccount{account}, lastMouseTarget: "account:one", lastMouseAt: time.Now()}
	updated, _ := m.Update(tea.MouseMsg{X: 3, Y: 7, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(tuiModel)
	if !m.authActive || m.authProviderID != providerDeepSeek || m.authReauthID != "one" {
		t.Fatalf("double click did not reauthenticate: %#v", m)
	}
}

func TestTUIProviderMetricsKeepResetsSeparate(t *testing.T) {
	used := 35.0
	reset := time.Now().Add(2 * time.Hour).Unix()
	m := tuiModel{width: 110, height: 24, settings: defaultAppSettings(), rows: []usageRow{{
		Name: "OpenCode", ProviderID: providerOpenCodeGo, Provider: "OpenCode", Plan: "Go",
		Metrics: []providerMetric{{Kind: percentageMetric, Slot: sessionSlot, Label: "SESSION", Used: &used, ResetAt: &reset}, {Kind: percentageMetric, Slot: weeklySlot, Label: "WEEKLY", Used: &used}, {Kind: percentageMetric, Slot: monthlySlot, Label: "MONTHLY", Used: &used}},
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
	m := tuiModel{width: 110, height: 24, settings: defaultAppSettings(), rows: []usageRow{{
		Name: "DeepSeek", ProviderID: providerDeepSeek, Provider: "DeepSeek", Plan: "USD 12.50",
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
	m := tuiModel{
		tab: resetsTab, width: 100, height: 24,
		accounts:    []storedAccount{{ID: "one", Name: "One", Provider: providerOpenAICodex}},
		resetCache:  map[string]*resetCreditsPayload{},
		resetLoader: func(string) (*resetCreditsPayload, error) { return &resetCreditsPayload{}, nil },
	}
	updated, command := m.Update(tea.MouseMsg{X: 3, Y: 7, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(tuiModel)
	if command == nil || !m.resetLoading || m.resetAccountID != "one" {
		t.Fatalf("reset account click = loading %v, account %q, command %v", m.resetLoading, m.resetAccountID, command)
	}
}

func TestResetsTabHidesUnsupportedProviders(t *testing.T) {
	m := tuiModel{accounts: []storedAccount{{ID: "codex", Provider: providerOpenAICodex}, {ID: "deepseek", Provider: providerDeepSeek}}}
	accounts := m.resetAccounts()
	if len(accounts) != 1 || accounts[0].ID != "codex" {
		t.Fatalf("reset accounts = %#v", accounts)
	}
}

func TestAccountsTabColumnOrder(t *testing.T) {
	m := tuiModel{
		tab: accountsTab, width: 110, height: 24, settings: defaultAppSettings(),
		accounts: []storedAccount{{Name: "Personal", PlanType: strPtr("plus"), Provider: providerOpenAICodex, Email: strPtr("person@example.com")}},
	}
	view := ansi.Strip(m.View())
	nameIndex := strings.Index(view, "ACCOUNT NAME")
	providerIndex := strings.Index(view, "PROVIDER")
	planIndex := strings.Index(view, "PLAN")
	emailIndex := strings.Index(view, "EMAIL")
	if !(nameIndex >= 0 && nameIndex < providerIndex && providerIndex < planIndex && planIndex < emailIndex) {
		t.Fatalf("account columns are out of order:\n%s", view)
	}
}

func TestCompactAccountsTabShowsProviderBeforePlan(t *testing.T) {
	m := tuiModel{
		tab: accountsTab, width: 64, height: 24, settings: defaultAppSettings(),
		accounts: []storedAccount{{Name: "Personal", PlanType: strPtr("plus"), Provider: providerOpenAICodex, Email: strPtr("person@example.com")}},
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "OpenAI Codex · plus") {
		t.Fatalf("compact account metadata is out of order:\n%s", view)
	}
}

func TestResetSidebarCompactModeUsesSingleLineAccounts(t *testing.T) {
	m := tuiModel{
		accounts: []storedAccount{{Name: "Personal", Provider: providerOpenAICodex}},
		settings: appSettings{CompactMode: true},
	}
	sidebar := ansi.Strip(m.renderResetSidebar(30, 20))
	if !strings.Contains(sidebar, "Personal · OpenAI") {
		t.Fatalf("compact reset sidebar did not use one line:\n%s", sidebar)
	}
}

func TestTUISettingsSaveHasNoSuccessToast(t *testing.T) {
	m := tuiModel{tab: settingsTab, notice: "old notice"}
	updated, _ := m.Update(storeSavedMsg{action: "Settings saved"})
	m = updated.(tuiModel)
	if m.notice != "" {
		t.Fatalf("settings save notice = %q, want empty", m.notice)
	}
}

func TestTUIAutoRefreshIgnoresOldTimer(t *testing.T) {
	m := tuiModel{timerVersion: 3, initialized: true}
	updated, cmd := m.Update(autoRefreshTickMsg{version: 2})
	m = updated.(tuiModel)
	if m.loading || cmd != nil {
		t.Fatal("stale auto-refresh timer started a refresh")
	}

	updated, cmd = m.Update(autoRefreshTickMsg{version: 3})
	m = updated.(tuiModel)
	if !m.loading || cmd == nil {
		t.Fatal("current auto-refresh timer did not start a refresh")
	}
}

func TestResetConsumptionRequiresSecondConfirmationWithoutMutation(t *testing.T) {
	credit := resetCreditDetail{Status: "available", Title: "Full reset", GrantedAt: "g", ExpiresAt: "e"}
	m := tuiModel{tab: resetsTab, resetRowsFocused: true, resetPayload: &resetCreditsPayload{Credits: []resetCreditDetail{credit}}}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(tuiModel)
	if m.consumeArmed == "" || !strings.Contains(m.notice, "again") {
		t.Fatalf("first consume action did not arm confirmation: %#v", m)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(tuiModel)
	if m.consumeArmed != "" || !strings.Contains(m.notice, "not connected yet") {
		t.Fatalf("second consume action did not stop at placeholder: %#v", m)
	}
	if m.resetPayload.Credits[0].Status != "available" {
		t.Fatal("placeholder consumption mutated the reset credit")
	}
}

func TestTUIHelpDoesNotAdvertiseScriptCommands(t *testing.T) {
	m := tuiModel{showHelp: true, width: 90, height: 24}
	view := m.View()
	if strings.Contains(view, "scriptable") || strings.Contains(view, "accounts login") {
		t.Fatalf("help still contains script command information:\n%s", view)
	}
}

func TestFocusedTabFooterDescribesFocusControls(t *testing.T) {
	m := tuiModel{tabRowFocused: true}
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
	for _, tab := range []tuiTab{usageTab, resetsTab, accountsTab, settingsTab} {
		m := tuiModel{tab: tab}
		footer := ansi.Strip(m.renderFooter(44))
		if !strings.Contains(footer, "q quit") {
			t.Fatalf("tab %d footer hides quit binding: %s", tab, footer)
		}
	}

	m := tuiModel{tab: resetsTab, resetRowsFocused: true}
	if footer := ansi.Strip(m.renderFooter(44)); !strings.Contains(footer, "q quit") {
		t.Fatalf("focused reset footer hides quit binding: %s", footer)
	}
}

func TestAccountsTabSupportsReauthentication(t *testing.T) {
	m := tuiModel{tab: accountsTab, accounts: []storedAccount{{ID: "one", Name: "Personal", Provider: providerOpenAICodex}}}
	if footer := ansi.Strip(m.renderFooter(100)); !strings.Contains(footer, "r reauthenticate") {
		t.Fatalf("accounts footer does not document reauthentication: %s", footer)
	}
	updated, command := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = updated.(tuiModel)
	if command == nil || !m.authActive || !m.authLoading || m.authReauthID != "one" {
		t.Fatal("r did not start reauthentication")
	}
	if m.tab != accountsTab {
		t.Fatalf("tab changed to %d", m.tab)
	}
}

func TestTUIAuthenticationRendersDeviceCodeAndCancels(t *testing.T) {
	m := tuiModel{width: 80, height: 24, authActive: true, authLoading: true, authVersion: 1}
	updated, command := m.Update(authCodeLoadedMsg{code: &deviceUserCodeResponse{UserCode: "ABCD-EFGH"}, version: 1})
	m = updated.(tuiModel)
	if command == nil || !m.authLoading || m.authCode == nil {
		t.Fatalf("authorization code state = %#v", m)
	}
	view := ansi.Strip(m.View())
	for _, want := range []string{"Add account", deviceAuthVerificationURL, "ABCD-EFGH", "Waiting for approval", "Esc cancels"} {
		if !strings.Contains(view, want) {
			t.Fatalf("authentication view missing %q:\n%s", want, view)
		}
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(tuiModel)
	if m.authActive || m.authVersion != 2 {
		t.Fatalf("authentication was not cancelled: %#v", m)
	}
}

func TestTUIAPIKeyInputAcceptsPastedRunes(t *testing.T) {
	m := tuiModel{authActive: true, authProviderID: providerDeepSeek}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("sk-pasted-key")})
	m = updated.(tuiModel)
	if m.authAPIKeyInput != "sk-pasted-key" {
		t.Fatalf("API key input = %q", m.authAPIKeyInput)
	}
}

func TestTUIViewHasOneRowShellMargin(t *testing.T) {
	m := tuiModel{width: 80, height: 24, settings: defaultAppSettings()}
	view := m.View()
	if !strings.HasPrefix(view, "\n") || !strings.HasSuffix(view, "\n") {
		t.Fatalf("View() does not have a one-row vertical shell margin: %q", view)
	}
}

func TestTUIViewFitsCommonTerminalWidths(t *testing.T) {
	used := 99.0
	for _, width := range []int{44, 64, 100} {
		m := tuiModel{
			width: width, height: 24,
			settings: defaultAppSettings(),
			accounts: []storedAccount{{
				ID: "one", Name: "An account with an exceptionally long display name", Provider: providerOpenAICodex,
				Email: strPtr("a-very-long-address@example.com"), PlanType: strPtr("enterprise"),
			}},
			rows: []usageRow{{
				Name:  "An account with an exceptionally long display name",
				Email: "a-very-long-address@example.com", ProviderID: providerOpenAICodex, Provider: "OpenAI Codex", Plan: "enterprise",
				Metrics:      []providerMetric{{Kind: percentageMetric, Slot: sessionSlot, Label: "SESSION", Used: &used}, {Kind: percentageMetric, Slot: weeklySlot, Label: "WEEKLY", Used: &used}},
				ResetCredits: "12, earliest exp. in 24h", SupportsResetCredits: true,
			}},
		}
		m.resetPayload = &resetCreditsPayload{AvailableCount: 1, TotalEarnedCount: 1, Credits: []resetCreditDetail{{
			Status: "available", Title: "A reset credit with a very long descriptive title",
			GrantedAt: "2026-08-01T12:00:00Z", ExpiresAt: "2026-08-09T12:00:00Z",
		}}}
		for _, tab := range []tuiTab{usageTab, resetsTab, accountsTab, settingsTab} {
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
	m := tuiModel{
		rows: []usageRow{{
			Name: "Personal", Email: "person@example.com", ProviderID: providerOpenAICodex, Provider: "OpenAI Codex", Plan: "plus",
			Metrics:      []providerMetric{{Kind: percentageMetric, Slot: sessionSlot, Label: "SESSION", Used: &primary}, {Kind: percentageMetric, Slot: weeklySlot, Label: "WEEKLY", Used: &secondary}},
			ResetCredits: "2, earliest exp. in 1d", SupportsResetCredits: true,
		}},
		width: 64, height: 24,
	}

	view := m.View()
	for _, want := range []string{"AI", "USAGE", "Personal", "OpenAI", "SESSION", "WEEKLY", "MONTHLY", "42%", "73%", "person@example.com"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() missing %q:\n%s", want, view)
		}
	}
}

func TestUsageDetailsHaveOneBlankRowAbove(t *testing.T) {
	used := 25.0
	for _, compact := range []bool{false, true} {
		m := tuiModel{
			width: 110, height: 24,
			settings: appSettings{UsageDisplay: "used", BarFill: "left", PercentagePosition: "right", ColorTheme: "default", CompactMode: compact},
			rows: []usageRow{{
				Name: "Personal", Email: "person@example.com", ProviderID: providerOpenAICodex, Provider: "OpenAI Codex", Plan: "plus",
				Metrics: []providerMetric{{Kind: percentageMetric, Slot: sessionSlot, Label: "SESSION", Used: &used}, {Kind: percentageMetric, Slot: weeklySlot, Label: "WEEKLY", Used: &used}}, ResetCredits: "0", SupportsResetCredits: true,
			}},
		}
		list := strings.TrimRight(m.renderAccountList(110, 22), "\n")
		want := list + "\n\n" + m.renderDetails(110)
		if got := m.renderUsageTab(110, 22); got != want {
			t.Fatalf("compact=%v detail spacing differs", compact)
		}
	}
}

func TestVisibleRangeKeepsCursorOnScreen(t *testing.T) {
	start, end := visibleRange(20, 18, 5)
	if start != 15 || end != 20 {
		t.Fatalf("visibleRange() = (%d, %d), want (15, 20)", start, end)
	}
}

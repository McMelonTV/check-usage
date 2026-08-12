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
		accounts:       []storedAccount{{ID: "one", Name: "One"}, {ID: "two", Name: "Two"}},
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
		accounts:       []storedAccount{{ID: "one", Name: "One", Provider: "OpenAI"}},
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
		rows:     []usageRow{{Name: "Personal", Provider: "OpenAI", Plan: "free", PrimaryUsed: &used, SecondaryUsed: &used, Stale: true}},
	}
	if view := ansi.Strip(m.renderUsageTab(106, 22)); !strings.Contains(view, "(Stale)") {
		t.Fatalf("stale usage row is not labeled:\n%s", view)
	}
}

func TestUsageListShowsAuthenticationFailureOnce(t *testing.T) {
	m := tuiModel{
		width: 110, height: 24, settings: defaultAppSettings(),
		rows: []usageRow{{Name: "Personal", Provider: "OpenAI", Plan: "free", AuthRequired: true}},
	}
	view := ansi.Strip(m.renderUsageTab(106, 22))
	if count := strings.Count(view, "sign in required"); count != 1 {
		t.Fatalf("authentication status appears %d times:\n%s", count, view)
	}
	if !strings.Contains(view, "Open Accounts and press a to reconnect.") {
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

func TestAccountsTabColumnOrder(t *testing.T) {
	m := tuiModel{
		tab: accountsTab, width: 110, height: 24, settings: defaultAppSettings(),
		accounts: []storedAccount{{Name: "Personal", PlanType: strPtr("plus"), Provider: "OpenAI", Email: strPtr("person@example.com")}},
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
		accounts: []storedAccount{{Name: "Personal", PlanType: strPtr("plus"), Provider: "OpenAI", Email: strPtr("person@example.com")}},
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "OpenAI · plus") {
		t.Fatalf("compact account metadata is out of order:\n%s", view)
	}
}

func TestResetSidebarCompactModeUsesSingleLineAccounts(t *testing.T) {
	m := tuiModel{
		accounts: []storedAccount{{Name: "Personal", Provider: "OpenAI"}},
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
	m := tuiModel{tab: accountsTab, accounts: []storedAccount{{ID: "one", Name: "Personal"}}}
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
				ID: "one", Name: "An account with an exceptionally long display name", Provider: "OpenAI",
				Email: strPtr("a-very-long-address@example.com"), PlanType: strPtr("enterprise"),
			}},
			rows: []usageRow{{
				Name:  "An account with an exceptionally long display name",
				Email: "a-very-long-address@example.com", Provider: "OpenAI", Plan: "enterprise",
				Primary:     "99% used / 1% left - resets in 1h 20m (August 6, 6:30 PM CEST)",
				Secondary:   "99% used / 1% left - resets in 120h (August 11, 5:00 PM CEST)",
				PrimaryUsed: &used, SecondaryUsed: &used, ResetCredits: "12, earliest exp. in 24h",
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
			Name: "Personal", Email: "person@example.com", Provider: "OpenAI", Plan: "plus",
			Primary: "42% used / 58% left", Secondary: "73% used / 27% left",
			PrimaryUsed: &primary, SecondaryUsed: &secondary, ResetCredits: "2, earliest exp. in 1d",
		}},
		width: 64, height: 24,
	}

	view := m.View()
	for _, want := range []string{"CODEX", "USAGE", "Personal", "OpenAI", "5H", "WK", "42%", "73%", "person@example.com"} {
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
				Name: "Personal", Email: "person@example.com", Provider: "OpenAI", Plan: "plus",
				Primary: "25% used", Secondary: "25% used", PrimaryUsed: &used, SecondaryUsed: &used, ResetCredits: "0",
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

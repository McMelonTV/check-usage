package main

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	tuiMaxWidth   = 120
	tuiWideAt     = 98
	tuiDetailAt   = 19
	tuiMinListLen = 1
)

type tuiTab int

const (
	usageTab tuiTab = iota
	resetsTab
	accountsTab
	settingsTab
	tuiTabCount
)

var (
	accentColor   = lipgloss.AdaptiveColor{Light: "#087F72", Dark: "#63D8C5"}
	textColor     = lipgloss.AdaptiveColor{Light: "#20242C", Dark: "#E7EAF0"}
	mutedColor    = lipgloss.AdaptiveColor{Light: "#68707D", Dark: "#7F8794"}
	borderColor   = lipgloss.AdaptiveColor{Light: "#CFD5DD", Dark: "#333A46"}
	greenColor    = lipgloss.AdaptiveColor{Light: "#16824F", Dark: "#6DDBA2"}
	amberColor    = lipgloss.AdaptiveColor{Light: "#A15C00", Dark: "#F3B45A"}
	redColor      = lipgloss.AdaptiveColor{Light: "#B42335", Dark: "#FF7585"}
	blueColor     = lipgloss.AdaptiveColor{Light: "#1769AA", Dark: "#6CB6FF"}
	orangeColor   = lipgloss.AdaptiveColor{Light: "#B54708", Dark: "#FFB86B"}
	magentaColor  = lipgloss.AdaptiveColor{Light: "#A53A87", Dark: "#F18ACB"}
	dimTrackColor = lipgloss.AdaptiveColor{Light: "#DDE2E8", Dark: "#303743"}

	tuiTitleStyle  = lipgloss.NewStyle().Bold(true).Foreground(textColor)
	tuiAccentStyle = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
	tuiMutedStyle  = lipgloss.NewStyle().Foreground(mutedColor)
	tuiErrorStyle  = lipgloss.NewStyle().Bold(true).Foreground(redColor)
	tuiBorderStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(borderColor).Padding(0, 1)
)

type tuiDataLoader func() ([]usageRow, []storedAccount, appSettings, map[string]*resetCreditsPayload, error)
type resetLoader func(string) (*resetCreditsPayload, error)

type dashboardLoadedMsg struct {
	rows       []usageRow
	accounts   []storedAccount
	settings   appSettings
	resetCache map[string]*resetCreditsPayload
	err        error
	at         time.Time
}

type resetLoadedMsg struct {
	accountID string
	payload   *resetCreditsPayload
	err       error
}

type storeSavedMsg struct {
	action string
	reload bool
	err    error
}

type authCodeLoadedMsg struct {
	code    *deviceUserCodeResponse
	err     error
	version int
}

type authCompletedMsg struct {
	account storedAccount
	err     error
	version int
}

type authSavedMsg struct {
	err     error
	version int
}
type autoRefreshTickMsg struct{ version int }
type spinnerTickMsg struct{}

type tuiModel struct {
	accountsPath string
	client       *http.Client
	loader       tuiDataLoader
	resetLoader  resetLoader

	rows           []usageRow
	accounts       []storedAccount
	settings       appSettings
	tab            tuiTab
	cursor         int
	tabCursors     [tuiTabCount]int
	creditCursor   int
	settingsCursor int

	width         int
	height        int
	loading       bool
	initialized   bool
	showHelp      bool
	tabRowFocused bool
	spinnerStep   int
	lastUpdated   time.Time
	err           error
	notice        string

	resetPayload     *resetCreditsPayload
	resetCache       map[string]*resetCreditsPayload
	resetAccountID   string
	resetLoading     bool
	resetErr         error
	resetRowsFocused bool
	consumeArmed     string
	removeArmed      string

	editingName  bool
	nameInput    string
	timerVersion int

	authActive            bool
	authLoading           bool
	authSaving            bool
	authCode              *deviceUserCodeResponse
	authErr               error
	authReauthID          string
	authVersion           int
	authProviderID        string
	authSelectingProvider bool
	authAPIKeyInput       string
	lastMouseTarget       string
	lastMouseAt           time.Time
}

func newTUIModel(accountsPath string, client *http.Client) tuiModel {
	m := tuiModel{
		accountsPath: accountsPath,
		client:       client,
		loading:      true,
		settings:     defaultAppSettings(),
	}
	m.loader = func() ([]usageRow, []storedAccount, appSettings, map[string]*resetCreditsPayload, error) {
		store, err := loadAccountsOrEmpty(accountsPath)
		if err != nil {
			return nil, nil, appSettings{}, nil, err
		}
		if store.needsSave {
			if err := saveAccounts(accountsPath, store); err != nil {
				return nil, nil, appSettings{}, nil, err
			}
		}

		rows, err := collectUsageRows(accountsPath, client)
		if err != nil {
			return nil, nil, appSettings{}, nil, err
		}
		store, err = loadAccountsOrEmpty(accountsPath)
		if err != nil {
			return nil, nil, appSettings{}, nil, err
		}
		settings, err := loadSettings(accountsPath)
		if err != nil {
			return nil, nil, appSettings{}, nil, err
		}
		cache, err := loadUsageCache(store.Accounts)
		if err != nil {
			return nil, nil, appSettings{}, nil, err
		}
		accounts := append([]storedAccount(nil), store.Accounts...)
		sort.SliceStable(accounts, func(i, j int) bool {
			return strings.ToLower(accounts[i].Name) < strings.ToLower(accounts[j].Name)
		})
		return rows, accounts, settings, resetPayloadCache(cache), nil
	}
	m.resetLoader = func(accountID string) (*resetCreditsPayload, error) {
		return loadResetCredits(accountsPath, accountID, client)
	}
	store, err := loadAccountsOrEmpty(accountsPath)
	if err != nil {
		m.err = err
		return m
	}
	if store.needsSave {
		if err := saveAccounts(accountsPath, store); err != nil {
			m.err = err
			return m
		}
	}
	settings, err := loadSettings(accountsPath)
	if err != nil {
		m.err = err
		return m
	}
	cache, err := loadUsageCache(store.Accounts)
	if err != nil {
		m.err = err
		return m
	}
	m.accounts = append([]storedAccount(nil), store.Accounts...)
	sort.SliceStable(m.accounts, func(i, j int) bool {
		return strings.ToLower(m.accounts[i].Name) < strings.ToLower(m.accounts[j].Name)
	})
	m.rows, m.lastUpdated = cachedUsageRows(store.Accounts, cache, time.Now())
	m.resetCache = resetPayloadCache(cache)
	m.settings = settings
	m.initialized = true
	return m
}

func runTUI(accountsPath string, client *http.Client) error {
	program := tea.NewProgram(newTUIModel(accountsPath, client), tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := program.Run()
	return err
}

func resetPayloadCache(cache map[string]usageCacheEntry) map[string]*resetCreditsPayload {
	payloads := make(map[string]*resetCreditsPayload)
	for accountID, entry := range cache {
		if entry.ResetCredits != nil {
			payloads[accountID] = entry.ResetCredits
		}
	}
	return payloads
}

func loadResetCredits(accountsPath, accountID string, client *http.Client) (*resetCreditsPayload, error) {
	store, err := loadAccounts(accountsPath)
	if err != nil {
		return nil, err
	}
	idx, err := findAccountByIDNameOrEmail(store.Accounts, accountID)
	if err != nil {
		return nil, err
	}
	account := store.Accounts[idx]
	provider, err := providerFor(account.Provider)
	if err != nil {
		return nil, err
	}
	if !provider.ResetCredits {
		return nil, fmt.Errorf("reset credits are unavailable for %s accounts", provider.Name)
	}
	updated, changed, err := ensureFreshTokens(account, client)
	if err != nil {
		return nil, err
	}
	if changed {
		store.Accounts[idx] = updated
		if err := saveAccounts(accountsPath, store); err != nil {
			return nil, err
		}
	}
	payload, err := fetchResetCredits(updated, client)
	if err != nil {
		return nil, err
	}
	if err := updateAccountResetCache(account.ID, payload, time.Now().Unix()); err != nil {
		return nil, err
	}
	return payload, nil
}

func (m tuiModel) Init() tea.Cmd {
	return tea.Batch(m.loadDashboard(), spinnerTick())
}

func (m tuiModel) loadDashboard() tea.Cmd {
	loader := m.loader
	return func() tea.Msg {
		rows, accounts, settings, resetCache, err := loader()
		return dashboardLoadedMsg{rows: rows, accounts: accounts, settings: settings, resetCache: resetCache, err: err, at: time.Now()}
	}
}

func (m tuiModel) loadSelectedResets() tea.Cmd {
	account, ok := m.selectedAccount()
	if !ok || m.resetLoader == nil {
		return nil
	}
	loader := m.resetLoader
	return func() tea.Msg {
		payload, err := loader(account.ID)
		return resetLoadedMsg{accountID: account.ID, payload: payload, err: err}
	}
}

func spinnerTick() tea.Cmd {
	return tea.Tick(90*time.Millisecond, func(time.Time) tea.Msg { return spinnerTickMsg{} })
}

func (m tuiModel) scheduleAutoRefresh() tea.Cmd {
	if m.settings.AutoRefreshSeconds <= 0 {
		return nil
	}
	version := m.timerVersion
	duration := time.Duration(m.settings.AutoRefreshSeconds) * time.Second
	return tea.Tick(duration, func(time.Time) tea.Msg { return autoRefreshTickMsg{version: version} })
}

func (m tuiModel) beginRefresh() (tuiModel, tea.Cmd) {
	if m.loading {
		return m, nil
	}
	m.loading = true
	m.err = nil
	m.notice = ""
	m.spinnerStep = 0
	m.timerVersion++
	return m, tea.Batch(m.loadDashboard(), spinnerTick())
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.editingName {
		return m.updateNameEditor(msg)
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height

	case dashboardLoadedMsg:
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.rows = msg.rows
			m.accounts = msg.accounts
			m.resetCache = msg.resetCache
			if m.resetAccountID != "" && !m.resetLoading && m.resetCache[m.resetAccountID] != nil {
				m.resetPayload = m.resetCache[m.resetAccountID]
				sortResetCredits(m.resetPayload.Credits)
			}
			if !m.initialized {
				m.settings = msg.settings
			}
			m.initialized = true
			m.lastUpdated = msg.at
			m.clampCursor()
			if m.tab == resetsTab && m.resetAccountID == "" && len(m.resetAccounts()) > 0 {
				m.showFocusedResetCache()
				m.resetLoading = true
				return m, tea.Batch(m.scheduleAutoRefresh(), m.loadSelectedResets(), spinnerTick())
			}
		}
		return m, m.scheduleAutoRefresh()

	case resetLoadedMsg:
		if msg.payload != nil {
			sortResetCredits(msg.payload.Credits)
		}
		if msg.err == nil && msg.payload != nil {
			if m.resetCache == nil {
				m.resetCache = make(map[string]*resetCreditsPayload)
			}
			m.resetCache[msg.accountID] = msg.payload
		}
		if m.resetAccountID == msg.accountID {
			m.resetLoading = false
			m.resetAccountID = msg.accountID
			if msg.payload != nil {
				m.resetPayload = msg.payload
			}
			m.resetErr = msg.err
			m.creditCursor = 0
		}

	case storeSavedMsg:
		if msg.err != nil {
			m.notice = "Couldn’t save: " + msg.err.Error()
			return m, nil
		}
		if !msg.reload {
			m.notice = ""
			return m, nil
		}
		m.notice = msg.action
		m.removeArmed = ""
		m.loading = true
		m.timerVersion++
		return m, tea.Batch(m.loadDashboard(), spinnerTick())

	case authCodeLoadedMsg:
		if msg.version != m.authVersion {
			return m, nil
		}
		m.authLoading = false
		m.authCode, m.authErr = msg.code, msg.err
		if msg.err != nil {
			return m, nil
		}
		m.authLoading = true
		return m, m.pollDeviceAuthorization(msg.code)

	case authCompletedMsg:
		if msg.version != m.authVersion {
			return m, nil
		}
		m.authLoading = false
		if msg.err != nil {
			m.authErr = msg.err
			return m, nil
		}
		m.authSaving = true
		return m, m.saveAuthenticatedAccount(msg.account)

	case authSavedMsg:
		if msg.version != m.authVersion {
			return m, nil
		}
		m.authSaving = false
		if msg.err != nil {
			m.authErr = msg.err
			return m, nil
		}
		m.clearAuthentication()
		m.notice = "Authentication finished"
		m.loading = true
		m.timerVersion++
		return m, tea.Batch(m.loadDashboard(), spinnerTick())

	case autoRefreshTickMsg:
		if msg.version == m.timerVersion && !m.loading {
			updated, cmd := m.beginRefresh()
			return updated, cmd
		}

	case spinnerTickMsg:
		if m.loading || m.resetLoading || m.authActive {
			m.spinnerStep++
			return m, spinnerTick()
		}

	case tea.MouseMsg:
		if m.authActive {
			return m.updateAuthMouse(msg)
		}
		return m.updateMouse(msg)

	case tea.KeyMsg:
		key := msg.String()
		if m.authActive {
			if m.authSelectingProvider {
				return m.updateProviderSelection(key)
			}
			if m.authProviderUsesAPIKey() {
				return m.updateAPIKeyAuthentication(msg)
			}
			if key == "esc" {
				m.authVersion++
				m.clearAuthentication()
			}
			return m, nil
		}
		switch key {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "?":
			m.showHelp = !m.showHelp
			return m, nil
		case "esc":
			if m.tab == resetsTab && m.resetRowsFocused {
				m.resetRowsFocused = false
				m.consumeArmed, m.notice = "", ""
				return m, nil
			}
			m.showHelp = false
			m.consumeArmed, m.removeArmed, m.notice = "", "", ""
			return m, nil
		}
		if m.showHelp {
			return m, nil
		}
		if key == "tab" {
			m.tabRowFocused = !m.tabRowFocused
			return m, nil
		}
		if m.tabRowFocused && (key == "left" || key == "right") {
			direction := 1
			if key == "left" {
				direction = -1
			}
			m.tabCursors[m.tab] = m.cursor
			m.tab = tuiTab((int(m.tab) + direction + int(tuiTabCount)) % int(tuiTabCount))
			m.cursor = m.tabCursors[m.tab]
			m.clampCursor()
			m.consumeArmed, m.removeArmed, m.notice = "", "", ""
			if m.tab == resetsTab && len(m.resetAccounts()) > 0 {
				m.resetRowsFocused = false
				m.showFocusedResetCache()
				m.resetLoading = true
				return m, tea.Batch(m.loadSelectedResets(), spinnerTick())
			}
			return m, nil
		}
		if m.tabRowFocused {
			if key == "up" || key == "k" || key == "down" || key == "j" {
				m.tabRowFocused = false
				return m.updateActiveTab(key)
			}
			return m, nil
		}
		if key == "r" && m.tab != accountsTab {
			if m.tab == resetsTab && !m.loading {
				m.resetPayload, m.resetErr, m.resetAccountID = nil, nil, ""
				m.resetLoading = true
			}
			updated, cmd := m.beginRefresh()
			return updated, cmd
		}
		return m.updateActiveTab(key)
	}

	return m, nil
}

func (m tuiModel) updateActiveTab(key string) (tea.Model, tea.Cmd) {
	switch m.tab {
	case usageTab:
		m.cursor = moveCursor(m.cursor, len(m.rows), key)

	case resetsTab:
		if !m.resetRowsFocused {
			before := m.cursor
			m.cursor = moveCursor(m.cursor, len(m.resetAccounts()), key)
			if before != m.cursor {
				m.showFocusedResetCache()
			}
			if key == "enter" || key == "right" {
				account, ok := m.selectedAccount()
				if !ok {
					return m, nil
				}
				m.resetRowsFocused = true
				m.creditCursor = 0
				m.consumeArmed, m.notice = "", ""
				m.resetAccountID = account.ID
				m.resetLoading = true
				return m, tea.Batch(m.loadSelectedResets(), spinnerTick())
			}
			return m, nil
		}
		creditCount := 0
		if m.resetPayload != nil {
			creditCount = len(m.resetPayload.Credits)
		}
		before := m.creditCursor
		m.creditCursor = moveCursor(m.creditCursor, creditCount, key)
		if before != m.creditCursor {
			m.consumeArmed, m.notice = "", ""
		}
		if key == "left" {
			m.resetRowsFocused = false
			m.consumeArmed, m.notice = "", ""
		} else if key == "enter" {
			m.armResetConsumption()
		}

	case accountsTab:
		before := m.cursor
		m.cursor = moveCursor(m.cursor, len(m.accounts), key)
		if before != m.cursor {
			m.removeArmed, m.notice = "", ""
		}
		switch key {
		case "a":
			return m.startAccountAdd()
		case "r":
			if account, ok := m.selectedAccount(); ok {
				return m.startAccountReauthentication(account)
			}
		case "e":
			if account, ok := m.selectedAccount(); ok {
				m.editingName = true
				m.nameInput = account.Name
				m.notice = ""
			}
		case "d":
			if account, ok := m.selectedAccount(); ok {
				if m.removeArmed != account.ID {
					m.removeArmed = account.ID
					m.notice = "Press d again to remove " + account.Name
				} else {
					return m, removeAccountCmd(m.accountsPath, account.ID, account.Name)
				}
			}
		}

	case settingsTab:
		if key == "up" || key == "k" {
			m.settingsCursor = max(0, m.settingsCursor-1)
			return m, nil
		}
		if key == "down" || key == "j" {
			m.settingsCursor = min(5, m.settingsCursor+1)
			return m, nil
		}
		if key == "left" || key == "right" || key == "[" || key == "]" || key == "enter" || key == " " {
			direction := 1
			if key == "left" || key == "[" {
				direction = -1
			}
			if m.settingsCursor == 0 {
				if m.settings.UsageDisplay == "used" {
					m.settings.UsageDisplay = "remaining"
				} else {
					m.settings.UsageDisplay = "used"
				}
				return m, saveSettingsCmd(m.accountsPath, m.settings)
			} else if m.settingsCursor == 1 {
				if m.settings.BarFill == "left" {
					m.settings.BarFill = "right"
				} else {
					m.settings.BarFill = "left"
				}
				return m, saveSettingsCmd(m.accountsPath, m.settings)
			} else if m.settingsCursor == 2 {
				if m.settings.PercentagePosition == "left" {
					m.settings.PercentagePosition = "right"
				} else {
					m.settings.PercentagePosition = "left"
				}
				return m, saveSettingsCmd(m.accountsPath, m.settings)
			} else if m.settingsCursor == 3 {
				m.settings.ColorTheme = nextColorTheme(m.settings.ColorTheme, direction)
				return m, saveSettingsCmd(m.accountsPath, m.settings)
			} else if m.settingsCursor == 4 {
				m.settings.AutoRefreshSeconds = nextRefreshInterval(m.settings.AutoRefreshSeconds, direction)
				m.timerVersion++
			} else {
				m.settings.CompactMode = !m.settings.CompactMode
				return m, saveSettingsCmd(m.accountsPath, m.settings)
			}
			return m, tea.Batch(saveSettingsCmd(m.accountsPath, m.settings), m.scheduleAutoRefresh())
		}
	}
	return m, nil
}

func (m tuiModel) startAccountAdd() (tea.Model, tea.Cmd) {
	m.authVersion++
	m.authActive, m.authSelectingProvider, m.authLoading, m.authSaving = true, true, false, false
	m.authProviderID, m.authAPIKeyInput, m.authCode, m.authErr, m.authReauthID = "", "", nil, nil, ""
	return m, nil
}

func (m tuiModel) startAccountReauthentication(account storedAccount) (tea.Model, tea.Cmd) {
	provider, err := providerFor(account.Provider)
	if err != nil {
		m.notice = err.Error()
		return m, nil
	}
	if provider.Credentials == apiKeyCredentials {
		m.authVersion++
		m.authActive, m.authSelectingProvider, m.authLoading, m.authSaving = true, false, false, false
		m.authProviderID, m.authAPIKeyInput, m.authCode, m.authErr, m.authReauthID = provider.ID, "", nil, nil, account.ID
		return m, nil
	}
	m.authVersion++
	m.authActive, m.authLoading, m.authSaving = true, true, false
	m.authSelectingProvider, m.authProviderID, m.authAPIKeyInput = false, provider.ID, ""
	m.authCode, m.authErr, m.authReauthID = nil, nil, account.ID
	return m, m.requestDeviceAuthorization()
}

func (m tuiModel) updateProviderSelection(key string) (tea.Model, tea.Cmd) {
	if key == "esc" {
		m.clearAuthentication()
		return m, nil
	}
	current := -1
	for index, provider := range providerDefinitions() {
		if provider.ID == m.authProviderID {
			current = index
			break
		}
	}
	if key == "left" || key == "up" || key == "h" || key == "k" {
		if current < 0 {
			current = len(providerDefinitions())
		}
		current = (current - 1 + len(providerDefinitions())) % len(providerDefinitions())
		m.authProviderID = providerDefinitions()[current].ID
		return m, nil
	}
	if key == "right" || key == "down" || key == "l" || key == "j" {
		current = (current + 1) % len(providerDefinitions())
		m.authProviderID = providerDefinitions()[current].ID
		return m, nil
	}
	if key != "enter" {
		return m, nil
	}
	if m.authProviderID == "" {
		m.authErr = fmt.Errorf("choose a provider first")
		return m, nil
	}
	provider, _ := providerFor(m.authProviderID)
	m.authSelectingProvider = false
	if provider.Credentials == apiKeyCredentials {
		return m, nil
	}
	m.authLoading = true
	return m, m.requestDeviceAuthorization()
}

func (m tuiModel) updateAPIKeyAuthentication(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.authSaving {
		if key.String() == "esc" {
			m.authVersion++
			m.clearAuthentication()
		}
		return m, nil
	}
	switch key.String() {
	case "esc":
		m.clearAuthentication()
	case "enter":
		if strings.TrimSpace(m.authAPIKeyInput) == "" {
			m.authErr = fmt.Errorf("API key cannot be empty")
			return m, nil
		}
		m.authSaving = true
		return m, m.saveAPIKeyAccount()
	case "backspace":
		runes := []rune(m.authAPIKeyInput)
		if len(runes) > 0 {
			m.authAPIKeyInput = string(runes[:len(runes)-1])
		}
	default:
		if key.Type == tea.KeyRunes {
			m.authAPIKeyInput += string(key.Runes)
		}
	}
	return m, nil
}

func (m *tuiModel) clearAuthentication() {
	m.authActive, m.authSelectingProvider, m.authLoading, m.authSaving = false, false, false, false
	m.authCode, m.authErr, m.authReauthID, m.authProviderID, m.authAPIKeyInput = nil, nil, "", "", ""
}

func (m tuiModel) authProviderUsesAPIKey() bool {
	provider, err := providerFor(m.authProviderID)
	return err == nil && provider.Credentials == apiKeyCredentials
}

func (m tuiModel) requestDeviceAuthorization() tea.Cmd {
	client := m.client
	version := m.authVersion
	return func() tea.Msg {
		code, err := requestDeviceUserCode(client)
		return authCodeLoadedMsg{code: code, err: err, version: version}
	}
}

func (m tuiModel) pollDeviceAuthorization(code *deviceUserCodeResponse) tea.Cmd {
	client := m.client
	version := m.authVersion
	name := ""
	if m.authReauthID != "" {
		if index, err := findAccountByIDNameOrEmail(m.accounts, m.authReauthID); err == nil {
			name = m.accounts[index].Name
		}
	}
	return func() tea.Msg {
		poll, err := pollDeviceToken(client, code)
		if err != nil {
			return authCompletedMsg{err: err, version: version}
		}
		tokens, err := exchangeAuthorizationCode(client, poll.AuthorizationCode, deviceAuthRedirectURI, poll.CodeVerifier)
		if err != nil {
			return authCompletedMsg{err: fmt.Errorf("device auth token exchange failed: %w", err), version: version}
		}
		email, planType, accountID := parseIDTokenClaims(tokens.IDToken)
		account := buildStoredAccount(name, email, planType, accountID, tokens)
		account.Provider = m.authProviderID
		return authCompletedMsg{account: account, version: version}
	}
}

func (m tuiModel) saveAuthenticatedAccount(account storedAccount) tea.Cmd {
	accountsPath, reauthID := m.accountsPath, m.authReauthID
	version := m.authVersion
	return func() tea.Msg {
		store, err := loadAccountsOrEmpty(accountsPath)
		if err != nil {
			return authSavedMsg{err: err, version: version}
		}
		if reauthID != "" {
			index, err := findAccountByIDNameOrEmail(store.Accounts, reauthID)
			if err != nil {
				return authSavedMsg{err: err, version: version}
			}
			existing := store.Accounts[index]
			if existing.Email != nil && account.Email != nil && !strings.EqualFold(strings.TrimSpace(*existing.Email), strings.TrimSpace(*account.Email)) {
				return authSavedMsg{err: fmt.Errorf("signed-in email does not match the selected account"), version: version}
			}
			if existing.Email != nil && account.Email == nil {
				return authSavedMsg{err: fmt.Errorf("signed-in account did not provide the expected email"), version: version}
			}
			if existing.AuthData.AccountID != nil && (account.AuthData.AccountID == nil || strings.TrimSpace(*existing.AuthData.AccountID) != strings.TrimSpace(*account.AuthData.AccountID)) {
				return authSavedMsg{err: fmt.Errorf("signed-in account does not match the selected account ID"), version: version}
			}
			account.ID, account.Name = existing.ID, existing.Name
			account.Provider = existing.Provider
			store.Accounts[index] = account
			if err := saveAccounts(accountsPath, store); err != nil {
				return authSavedMsg{err: err, version: version}
			}
			return authSavedMsg{err: removeAccountUsageCache(account.ID), version: version}
		}

		if index := findMatchingAccount(store.Accounts, account); index >= 0 {
			account.ID = store.Accounts[index].ID
			if strings.TrimSpace(store.Accounts[index].Name) != "" {
				account.Name = store.Accounts[index].Name
			}
			store.Accounts[index] = account
		} else {
			store.Accounts = append(store.Accounts, account)
		}
		return authSavedMsg{err: saveAccounts(accountsPath, store), version: version}
	}
}

func (m tuiModel) saveAPIKeyAccount() tea.Cmd {
	accountsPath, providerID, key, reauthID := m.accountsPath, m.authProviderID, m.authAPIKeyInput, m.authReauthID
	version := m.authVersion
	return func() tea.Msg {
		store, err := loadAccountsOrEmpty(accountsPath)
		if err != nil {
			return authSavedMsg{err: err, version: version}
		}
		if reauthID != "" {
			index, err := findAccountByIDNameOrEmail(store.Accounts, reauthID)
			if err != nil {
				return authSavedMsg{err: err, version: version}
			}
			store.Accounts[index].AuthData = authData{Type: string(apiKeyCredentials), APIKey: strPtr(key)}
			if err := saveAccounts(accountsPath, store); err != nil {
				return authSavedMsg{err: err, version: version}
			}
			return authSavedMsg{err: removeAccountUsageCache(store.Accounts[index].ID), version: version}
		}
		provider, err := providerFor(providerID)
		if err != nil {
			return authSavedMsg{err: err, version: version}
		}
		name := provider.Name
		for suffix := 2; accountNameExists(store.Accounts, name); suffix++ {
			name = fmt.Sprintf("%s %d", provider.Name, suffix)
		}
		store.Accounts = append(store.Accounts, storedAccount{ID: newAccountID(), Name: name, Provider: provider.ID, PlanType: optionalString(provider.Plan), AuthData: authData{Type: string(apiKeyCredentials), APIKey: strPtr(key)}})
		return authSavedMsg{err: saveAccounts(accountsPath, store), version: version}
	}
}

func accountNameExists(accounts []storedAccount, name string) bool {
	for _, account := range accounts {
		if strings.EqualFold(strings.TrimSpace(account.Name), strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}

func (m tuiModel) updateMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.showHelp {
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			m.showHelp = false
		}
		return m, nil
	}
	if msg.Button == tea.MouseButtonWheelUp {
		return m.updateActiveTab("up")
	}
	if msg.Button == tea.MouseButtonWheelDown {
		return m.updateActiveTab("down")
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}

	x := msg.X - tuiContentOrigin(m.width)
	if msg.Y == 2 {
		for i, right := range []int{8, 17, 28, 38} {
			if x < right {
				m.tab = tuiTab(i)
				m.tabRowFocused = false
				m.clampCursor()
				if m.tab == resetsTab && len(m.accounts) > 0 {
					m.resetRowsFocused = false
					if len(m.resetAccounts()) > 0 {
						m.showFocusedResetCache()
						m.resetLoading = true
						return m, tea.Batch(m.loadSelectedResets(), spinnerTick())
					}
				}
				return m, nil
			}
		}
	}

	switch m.tab {
	case usageTab:
		if index, ok := m.usageMouseRowIndex(msg.Y); ok {
			m.cursor = index
		}
	case resetsTab:
		sidebarWidth := max(10, min(24, tuiContentWidth(m.width)/3))
		if x < sidebarWidth {
			if index, ok := m.mouseResetAccountIndex(msg.Y); ok {
				m.cursor = index
				m.resetRowsFocused = false
				m.showFocusedResetCache()
				m.resetLoading = true
				return m, tea.Batch(m.loadSelectedResets(), spinnerTick())
			}
		} else if index, ok := m.mouseResetCreditIndex(msg.Y); ok {
			doubleClick := m.registerMouseClick("reset:" + resetCreditKey(m.resetPayload.Credits[index]))
			m.creditCursor, m.resetRowsFocused = index, true
			if doubleClick {
				m.armResetConsumption()
			} else {
				m.consumeArmed, m.notice = "", ""
			}
		}
	case accountsTab:
		if len(m.accounts) == 0 && msg.Y >= 5 {
			return m.startAccountAdd()
		}
		if index, ok := m.mouseRowIndex(msg.Y, len(m.accounts), m.width >= 80, 7, 5); ok {
			m.cursor = index
			if m.registerMouseClick("account:" + m.accounts[index].ID) {
				return m.startAccountReauthentication(m.accounts[index])
			}
		}
	case settingsTab:
		if index := (msg.Y - 5) / 2; msg.Y >= 5 && index >= 0 && index < 6 {
			m.settingsCursor = index
			direction := "left"
			if x >= tuiContentWidth(m.width)/2 {
				direction = "right"
			}
			return m.updateActiveTab(direction)
		}
	}
	return m, nil
}

func (m *tuiModel) registerMouseClick(target string) bool {
	now := time.Now()
	doubleClick := m.lastMouseTarget == target && now.Sub(m.lastMouseAt) <= 500*time.Millisecond
	m.lastMouseTarget, m.lastMouseAt = target, now
	return doubleClick
}

func (m tuiModel) updateAuthMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.authSelectingProvider && msg.Button == tea.MouseButtonWheelUp {
		return m.updateProviderSelection("up")
	}
	if m.authSelectingProvider && msg.Button == tea.MouseButtonWheelDown {
		return m.updateProviderSelection("down")
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	if m.authSelectingProvider {
		index := msg.Y - 9
		definitions := providerDefinitions()
		if index >= 0 && index < len(definitions) {
			m.authProviderID = definitions[index].ID
			return m.updateProviderSelection("enter")
		}
	}
	return m, nil
}

func (m tuiModel) usageMouseRowIndex(y int) (int, bool) {
	if m.width >= tuiWideAt {
		return m.mouseRowIndex(y, len(m.rows), true, 7, 5)
	}
	currentY := 5
	for index := range m.rows {
		height := 5
		if !m.settings.CompactMode {
			height++
		}
		if y >= currentY && y < currentY+height {
			return index, true
		}
		currentY += height
	}
	return 0, false
}

func (m tuiModel) mouseResetAccountIndex(y int) (int, bool) {
	stride := 1
	if !m.settings.CompactMode {
		stride = 3
	}
	if y < 7 {
		return 0, false
	}
	index := (y - 7) / stride
	return index, index >= 0 && index < len(m.resetAccounts())
}

func (m tuiModel) mouseResetCreditIndex(y int) (int, bool) {
	if m.resetPayload == nil || y < 10 {
		return 0, false
	}
	index := (y - 10) / 3
	return index, index >= 0 && index < len(m.resetPayload.Credits)
}

func tuiContentWidth(width int) int {
	if width <= 0 {
		width = 92
	}
	contentWidth := min(tuiMaxWidth, max(20, width-4))
	if width < 24 {
		return max(10, width)
	}
	return contentWidth
}

func tuiContentOrigin(width int) int {
	contentWidth := tuiContentWidth(width)
	if width > contentWidth {
		return (width - contentWidth) / 2
	}
	return 0
}

func (m tuiModel) mouseRowIndex(y, total int, wide bool, wideStart, compactStart int) (int, bool) {
	if total == 0 {
		return 0, false
	}
	startY, stride := compactStart, 3
	if wide {
		startY, stride = wideStart, 1
	}
	if !m.settings.CompactMode {
		stride++
	}
	if y < startY || (y-startY)%stride != 0 {
		return 0, false
	}
	maxRows := max(tuiMinListLen, (m.height-15)/stride)
	start, end := visibleRange(total, m.cursor, maxRows)
	index := start + (y-startY)/stride
	return index, index < end
}

func (m tuiModel) updateNameEditor(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.editingName, m.nameInput = false, ""
	case "enter":
		name := strings.TrimSpace(m.nameInput)
		account, selected := m.selectedAccount()
		if !selected || name == "" {
			m.notice = "Account name cannot be empty"
			return m, nil
		}
		m.editingName = false
		return m, renameAccountCmd(m.accountsPath, account.ID, name)
	case "backspace":
		runes := []rune(m.nameInput)
		if len(runes) > 0 {
			m.nameInput = string(runes[:len(runes)-1])
		}
	default:
		if key.Type == tea.KeyRunes {
			m.nameInput += string(key.Runes)
		}
	}
	return m, nil
}

func (m *tuiModel) armResetConsumption() {
	if m.resetPayload == nil || m.creditCursor < 0 || m.creditCursor >= len(m.resetPayload.Credits) {
		return
	}
	credit := m.resetPayload.Credits[m.creditCursor]
	if !resetCreditAvailable(credit) {
		m.notice = "Only available reset credits can be consumed"
		return
	}
	key := resetCreditKey(credit)
	if m.consumeArmed != key {
		m.consumeArmed = key
		m.notice = "Press Enter again to confirm claim"
		return
	}
	// Intentionally no API call until reset consumption is implemented.
	m.consumeArmed = ""
	m.notice = "Confirmed — reset claiming is not connected yet"
}

func (m tuiModel) selectedAccount() (storedAccount, bool) {
	accounts := m.accounts
	if m.tab == resetsTab {
		accounts = m.resetAccounts()
	}
	if m.cursor < 0 || m.cursor >= len(accounts) {
		return storedAccount{}, false
	}
	return accounts[m.cursor], true
}

func (m tuiModel) resetAccounts() []storedAccount {
	accounts := make([]storedAccount, 0, len(m.accounts))
	for _, account := range m.accounts {
		provider, err := providerFor(account.Provider)
		if err == nil && provider.ResetCredits {
			accounts = append(accounts, account)
		}
	}
	return accounts
}

func (m *tuiModel) clampCursor() {
	count := len(m.rows)
	if m.tab == resetsTab || m.tab == accountsTab {
		count = len(m.accounts)
		if m.tab == resetsTab {
			count = len(m.resetAccounts())
		}
	}
	if m.cursor >= count {
		m.cursor = max(0, count-1)
	}
}

func moveCursor(cursor, count int, key string) int {
	switch key {
	case "up", "k":
		return max(0, cursor-1)
	case "down", "j":
		return min(max(0, count-1), cursor+1)
	case "home", "g":
		return 0
	case "end", "G":
		return max(0, count-1)
	default:
		return cursor
	}
}

func nextRefreshInterval(current, direction int) int {
	options := []int{0, 30, 60, 300, 900}
	index := 0
	for i, option := range options {
		if option == current {
			index = i
			break
		}
	}
	return options[(index+direction+len(options))%len(options)]
}

func nextColorTheme(current string, direction int) string {
	options := []string{"default", "colorblind", "monochrome"}
	index := 0
	for i, option := range options {
		if option == current {
			index = i
			break
		}
	}
	return options[(index+direction+len(options))%len(options)]
}

func saveSettingsCmd(path string, settings appSettings) tea.Cmd {
	return func() tea.Msg {
		err := saveSettings(path, settings)
		return storeSavedMsg{err: err}
	}
}

func renameAccountCmd(path, accountID, newName string) tea.Cmd {
	return func() tea.Msg {
		store, err := loadAccountsOrEmpty(path)
		if err == nil {
			idx, findErr := findAccountByIDNameOrEmail(store.Accounts, accountID)
			err = findErr
			if err == nil {
				for i := range store.Accounts {
					if i != idx && strings.EqualFold(strings.TrimSpace(store.Accounts[i].Name), newName) {
						err = fmt.Errorf("account name %q already exists", newName)
						break
					}
				}
			}
			if err == nil {
				store.Accounts[idx].Name = newName
				err = saveAccounts(path, store)
			}
		}
		return storeSavedMsg{action: "Account renamed", reload: true, err: err}
	}
}

func removeAccountCmd(path, accountID, accountName string) tea.Cmd {
	return func() tea.Msg {
		store, err := loadAccountsOrEmpty(path)
		if err == nil {
			idx, findErr := findAccountByIDNameOrEmail(store.Accounts, accountID)
			err = findErr
			if err == nil {
				store.Accounts = append(store.Accounts[:idx], store.Accounts[idx+1:]...)
				err = saveAccounts(path, store)
				if err == nil {
					err = removeAccountUsageCache(accountID)
				}
			}
		}
		return storeSavedMsg{action: "Removed " + accountName, reload: true, err: err}
	}
}

func resetCreditKey(credit resetCreditDetail) string {
	return credit.GrantedAt + "\x00" + credit.ExpiresAt + "\x00" + credit.Title
}

func (m tuiModel) View() string {
	width, height := m.width, m.height
	if width <= 0 {
		width = 92
	}
	if height <= 0 {
		height = 24
	}
	innerHeight := max(1, height-2)
	contentWidth := min(tuiMaxWidth, max(20, width-4))
	if width < 24 {
		contentWidth = max(10, width)
	}

	sections := []string{m.renderHeader(contentWidth)}
	switch {
	case m.authActive:
		sections = append(sections, m.renderAuthentication(contentWidth, innerHeight))
	case m.showHelp:
		sections = append(sections, m.renderHelp(contentWidth))
	case m.loading && !m.initialized:
		sections = append(sections, m.renderLoading(contentWidth, innerHeight))
	case m.err != nil && !m.initialized:
		sections = append(sections, m.renderError(contentWidth, m.err))
	default:
		if m.err != nil {
			sections = append(sections, m.renderRefreshWarning(contentWidth, m.err))
		}
		switch m.tab {
		case usageTab:
			sections = append(sections, m.renderUsageTab(contentWidth, innerHeight))
		case resetsTab:
			sections = append(sections, m.renderResetsTab(contentWidth, innerHeight))
		case accountsTab:
			sections = append(sections, m.renderAccountsTab(contentWidth, innerHeight))
		case settingsTab:
			sections = append(sections, m.renderSettingsTab(contentWidth))
		}
	}
	sections = append(sections, m.renderFooter(contentWidth))
	view := lipgloss.JoinVertical(lipgloss.Left, sections...)
	if width > contentWidth {
		view = lipgloss.PlaceHorizontal(width, lipgloss.Center, view)
	}
	return "\n" + view + "\n"
}

func (m tuiModel) renderHeader(width int) string {
	brand := tuiTitleStyle.Render("AI") + " " + tuiAccentStyle.Render("USAGE")
	status := ""
	if m.loading {
		status = tuiAccentStyle.Render(spinnerFrame(m.spinnerStep) + " refreshing")
	} else if !m.lastUpdated.IsZero() {
		status = tuiMutedStyle.Render("updated " + m.lastUpdated.Format("15:04:05"))
	}
	if lipgloss.Width(brand)+lipgloss.Width(status)+2 > width {
		status = ""
	}
	header := brand + strings.Repeat(" ", max(1, width-lipgloss.Width(brand)-lipgloss.Width(status))) + status

	tabNames := []string{"Usage", "Resets", "Accounts", "Settings"}
	tabs := make([]string, 0, len(tabNames))
	for i, name := range tabNames {
		style := lipgloss.NewStyle().Foreground(mutedColor).Padding(0, 1)
		if tuiTab(i) == m.tab {
			style = style.Foreground(accentColor).Bold(true).Underline(true)
			if m.tabRowFocused {
				style = style.Reverse(true)
			}
		}
		tabs = append(tabs, style.Render(name))
	}
	tabLine := ansi.Truncate(strings.Join(tabs, " "), width, "…")
	rule := lipgloss.NewStyle().Foreground(borderColor).Render(strings.Repeat("─", width))
	return header + "\n" + tabLine + "\n" + rule
}

func (m tuiModel) renderLoading(width, height int) string {
	message := tuiAccentStyle.Render(spinnerFrame(m.spinnerStep)) + "  " + ansi.Truncate("Fetching usage across your accounts…", max(8, width-4), "…")
	space := max(2, min(8, height-8))
	return strings.Repeat("\n", space/2) + lipgloss.PlaceHorizontal(width, lipgloss.Center, message) + strings.Repeat("\n", space-space/2)
}

func (m tuiModel) renderAuthentication(width, height int) string {
	title := "Add account"
	if m.authReauthID != "" {
		title = "Reauthenticate account"
	}
	if m.authSelectingProvider {
		lines := []string{tuiMutedStyle.Render("Choose a provider:")}
		for _, provider := range providerDefinitions() {
			marker, style := "  ", tuiMutedStyle
			if provider.ID == m.authProviderID {
				marker, style = "› ", tuiAccentStyle
			}
			credential := "API key"
			if provider.Credentials == deviceCredentials {
				credential = "Device login"
			}
			lines = append(lines, marker+style.Render(provider.Name)+tuiMutedStyle.Render(" · "+credential))
		}
		if m.authErr != nil {
			lines = append(lines, "", tuiErrorStyle.Render(m.authErr.Error()))
		}
		lines = append(lines, "", tuiMutedStyle.Render("Enter select · Esc cancel"))
		return "\n" + tuiBorderStyle.Width(max(20, width-4)).Render(tuiTitleStyle.Render(title)+"\n\n"+strings.Join(lines, "\n"))
	}
	if m.authProviderUsesAPIKey() {
		provider, _ := providerFor(m.authProviderID)
		key := strings.Repeat("•", len([]rune(m.authAPIKeyInput)))
		input := tuiAccentStyle.Render("> ")
		if key == "" {
			placeholder := "API Key"
			if apiKeyCursorVisible(m.spinnerStep) {
				cursorStyle := lipgloss.NewStyle().Foreground(textColor).Background(accentColor)
				input += cursorStyle.Render(placeholder[:1]) + tuiMutedStyle.Render(placeholder[1:])
			} else {
				input += tuiMutedStyle.Render(placeholder)
			}
		} else {
			input += tuiAccentStyle.Render(key)
			if apiKeyCursorVisible(m.spinnerStep) {
				input += lipgloss.NewStyle().Background(accentColor).Render(" ")
			} else {
				input += " "
			}
		}
		body := tuiMutedStyle.Render("Enter API key for "+provider.Name+":") + "\n" + input + "\n\n" + tuiMutedStyle.Render("Enter save · Esc cancel")
		if m.authSaving {
			body = tuiAccentStyle.Render(spinnerFrame(m.spinnerStep)+"  Saving account…") + "\n\n" + body
		}
		if m.authErr != nil {
			body = tuiErrorStyle.Render(m.authErr.Error()) + "\n\n" + body
		}
		return "\n" + tuiBorderStyle.Width(max(20, width-4)).Render(tuiTitleStyle.Render(title)+"\n\n"+body)
	}
	if m.authErr != nil {
		body := tuiErrorStyle.Render("Authentication failed") + "\n" +
			tuiMutedStyle.Render(ansi.Truncate(m.authErr.Error(), max(16, width-8), "…")) + "\n\n" +
			tuiMutedStyle.Render("Press Esc to return to Accounts.")
		return "\n" + tuiBorderStyle.Width(max(20, width-4)).Render(tuiTitleStyle.Render(title)+"\n\n"+body)
	}
	if m.authCode == nil {
		return "\n" + tuiBorderStyle.Width(max(20, width-4)).Render(tuiTitleStyle.Render(title)+"\n\n"+tuiAccentStyle.Render(spinnerFrame(m.spinnerStep))+"  Requesting authorization code…")
	}
	status := tuiAccentStyle.Render(spinnerFrame(m.spinnerStep) + "  Waiting for approval…")
	if m.authSaving {
		status = tuiAccentStyle.Render(spinnerFrame(m.spinnerStep) + "  Saving account…")
	}
	code := lipgloss.NewStyle().Bold(true).Foreground(accentColor).Render(m.authCode.UserCode)
	body := strings.Join([]string{
		tuiMutedStyle.Render("Open this address in your browser:"),
		tuiAccentStyle.Render(deviceAuthVerificationURL),
		"",
		tuiMutedStyle.Render("Enter this code:"),
		code,
		"",
		status,
		tuiMutedStyle.Render("Esc cancels"),
	}, "\n")
	return "\n" + tuiBorderStyle.Width(max(20, width-4)).Render(tuiTitleStyle.Render(title)+"\n\n"+body)
}

func apiKeyCursorVisible(spinnerStep int) bool {
	return (spinnerStep/5)%2 == 0
}

func (m tuiModel) renderError(width int, err error) string {
	title := tuiErrorStyle.Render("Couldn’t load usage")
	body := tuiMutedStyle.Render(ansi.Truncate(err.Error(), max(16, width-8), "…"))
	return "\n" + tuiBorderStyle.Width(max(20, width-4)).Render(title+"\n"+body+"\n\nPress r to try again.")
}

func (m tuiModel) renderRefreshWarning(width int, err error) string {
	message := "Refresh failed · " + ansi.Truncate(err.Error(), max(8, width-22), "…") + " · showing last update"
	return "\n" + tuiErrorStyle.Render(ansi.Truncate(message, width, "…"))
}

func (m tuiModel) renderUsageTab(width, height int) string {
	if len(m.rows) == 0 {
		return m.renderEmpty(width, "No accounts yet", "Open the Accounts tab and press a to choose a provider.")
	}
	accountList := m.renderAccountList(width, height)
	if height >= tuiDetailAt {
		return strings.TrimRight(accountList, "\n") + "\n\n" + m.renderDetails(width)
	}
	return accountList
}

func (m tuiModel) renderEmpty(width int, title, body string) string {
	return "\n" + tuiBorderStyle.Width(max(20, width-4)).Render(tuiTitleStyle.Render(title)+"\n"+tuiMutedStyle.Render(body))
}

func (m tuiModel) renderAccountList(width, height int) string {
	if width >= tuiWideAt {
		return m.renderWideList(width, height)
	}
	return m.renderCompactList(width, height)
}

func (m tuiModel) renderWideList(width, height int) string {
	nameWidth := min(18, max(12, width/8))
	providerWidth, planWidth, creditWidth := 11, 10, 8
	usageWidth := max(12, (width-nameWidth-providerWidth-planWidth-creditWidth-8)/3)
	headerStyle := lipgloss.NewStyle().Foreground(mutedColor).Bold(true)
	header := "  " + cell(headerStyle.Render("ACCOUNT"), nameWidth) + " " +
		cell(headerStyle.Render("PROVIDER"), providerWidth) + " " + cell(headerStyle.Render("PLAN"), planWidth) + " " +
		cell(headerStyle.Render("SESSION"), usageWidth) + " " + cell(headerStyle.Render("WEEKLY"), usageWidth) + " " +
		cell(headerStyle.Render("MONTHLY"), usageWidth) + " " + cell(headerStyle.Render("RESETS"), creditWidth)

	rowStride := 1
	if !m.settings.CompactMode {
		rowStride = 2
	}
	maxRows := max(tuiMinListLen, (height-15)/rowStride)
	start, end := visibleRange(len(m.rows), m.cursor, maxRows)
	lines := []string{"", header, ""}
	for i := start; i < end; i++ {
		row := m.rows[i]
		marker, nameStyle := "  ", lipgloss.NewStyle().Foreground(textColor)
		if i == m.cursor {
			marker, nameStyle = selectedRowVisual(m.tabRowFocused)
		}
		name := row.Name
		if row.Stale || row.ResetsStale {
			name += " " + lipgloss.NewStyle().Foreground(amberColor).Render("(Stale)")
		}
		session := m.renderUsageSlot(row, sessionSlot, usageWidth)
		weekly := m.renderUsageSlot(row, weeklySlot, usageWidth)
		monthly := m.renderUsageSlot(row, monthlySlot, usageWidth)
		resets := m.renderResetSlot(row)
		line := marker + cell(nameStyle.Render(name), nameWidth) + " " +
			cell(tuiMutedStyle.Render(row.Provider), providerWidth) + " " + cell(tuiMutedStyle.Render(row.Plan), planWidth) + " " +
			cell(session, usageWidth) + " " + cell(weekly, usageWidth) + " " + cell(monthly, usageWidth) + " " + cell(resets, creditWidth)
		lines = append(lines, line)
		if !m.settings.CompactMode {
			lines = append(lines, "")
		}
	}
	if start > 0 || end < len(m.rows) {
		lines = append(lines, tuiMutedStyle.Render(fmt.Sprintf("  showing %d–%d of %d", start+1, end, len(m.rows))))
	}
	return strings.Join(lines, "\n")
}

func (m tuiModel) renderCompactList(width, height int) string {
	rowStride := 5
	if !m.settings.CompactMode {
		rowStride++
	}
	maxRows := max(tuiMinListLen, (height-15)/rowStride)
	start, end := visibleRange(len(m.rows), m.cursor, maxRows)
	lines := []string{""}
	for i := start; i < end; i++ {
		row := m.rows[i]
		marker, nameStyle := "  ", lipgloss.NewStyle().Foreground(textColor)
		if i == m.cursor {
			marker, nameStyle = selectedRowVisual(m.tabRowFocused)
		}
		metaText := strings.ToUpper(row.Provider) + " · " + strings.ToUpper(row.Plan)
		if row.Stale || row.ResetsStale {
			metaText += " · STALE"
		}
		meta := tuiMutedStyle.Render(metaText)
		visibleName := ansi.Truncate(row.Name, max(8, width-lipgloss.Width(meta)-6), "…")
		gap := max(1, width-2-lipgloss.Width(visibleName)-lipgloss.Width(meta))
		lines = append(lines, marker+nameStyle.Render(visibleName)+strings.Repeat(" ", gap)+meta)
		for _, slot := range []metricSlot{sessionSlot, weeklySlot, monthlySlot} {
			lines = append(lines, m.renderCompactSlot(row, slot, width))
		}
		lines = append(lines, "  "+tuiMutedStyle.Render("RESETS  ")+m.renderResetSlot(row))
		if !m.settings.CompactMode {
			lines = append(lines, "")
		}
	}
	return strings.Join(lines, "\n")
}

func (m tuiModel) renderDetails(width int) string {
	if m.cursor >= len(m.rows) {
		return ""
	}
	row := m.rows[m.cursor]
	labelStyle := lipgloss.NewStyle().Foreground(mutedColor).Width(9)
	name := tuiTitleStyle.Render(row.Name)
	if row.Stale || row.ResetsStale {
		name += "  " + lipgloss.NewStyle().Bold(true).Foreground(amberColor).Render("(Stale)")
	}
	nameLine := name + "  " + tuiMutedStyle.Render(row.Email+" · "+row.Provider+" · "+row.Plan)
	if row.AuthRequired {
		return tuiBorderStyle.Width(max(20, width-4)).Render(nameLine + "\n" + tuiErrorStyle.Render(credentialRequiredText(row)+". Open Accounts and press r to reconnect."))
	}
	lines := []string{nameLine}
	now := time.Now()
	for _, slot := range []metricSlot{sessionSlot, weeklySlot, monthlySlot} {
		label := strings.ToUpper(string(slot))
		lines = append(lines, labelStyle.Render(label)+ansi.Truncate(usageSlotText(row, slot, now), max(16, width-15), "…"))
	}
	resets := resetSlotText(row)
	if row.ResetsStale {
		resets += " (stale)"
	}
	lines = append(lines, labelStyle.Render("RESETS")+ansi.Truncate(resets, max(16, width-15), "…"))
	return tuiBorderStyle.Width(max(20, width-4)).Render(strings.Join(lines, "\n"))
}

func (m tuiModel) renderUsageSlot(row usageRow, slot metricSlot, width int) string {
	if metric, ok := usageMetricForSlot(row, slot); ok && metric.Kind == percentageMetric {
		return renderUsageBar(metric.Used, width, m.showRemaining(), row.Loading, false, row.Stale, m.settings.BarFill, m.settings.PercentagePosition, m.settings.ColorTheme)
	}
	text := usageSlotText(row, slot, time.Now())
	if row.AuthRequired && slot == sessionSlot {
		return tuiErrorStyle.Render(ansi.Truncate(text, width, "…"))
	}
	return tuiMutedStyle.Render(ansi.Truncate(text, width, "…"))
}

func (m tuiModel) renderCompactSlot(row usageRow, slot metricSlot, width int) string {
	label := tuiMutedStyle.Render(cell(strings.ToUpper(string(slot)), 8))
	valueWidth := max(10, width-12)
	return "  " + label + "  " + m.renderUsageSlot(row, slot, valueWidth)
}

func (m tuiModel) renderResetSlot(row usageRow) string {
	if row.SupportsResetCredits {
		return renderCreditCount(resetSlotText(row), row.ResetsStale, m.settings.ColorTheme)
	}
	return tuiMutedStyle.Render("-")
}

func credentialRequiredText(row usageRow) string {
	if row.ProviderID == providerCodex {
		return "Sign in required"
	}
	return "API key required or invalid"
}

func (m tuiModel) renderResetsTab(width, height int) string {
	if len(m.resetAccounts()) == 0 {
		return m.renderEmpty(width, "No reset-capable accounts", "Reset credits are available only for providers that support them.")
	}
	sidebarWidth := max(10, min(24, width/3))
	mainWidth := max(8, width-sidebarWidth-2)
	sidebar := lipgloss.NewStyle().Width(sidebarWidth).MaxWidth(sidebarWidth).Render(m.renderResetSidebar(sidebarWidth, height))
	main := lipgloss.NewStyle().Width(mainWidth).MaxWidth(mainWidth).Render(m.renderResetMain(mainWidth, height))
	paneHeight := max(lipgloss.Height(sidebar), lipgloss.Height(main))
	divider := lipgloss.NewStyle().Foreground(borderColor).Render(strings.TrimSuffix(strings.Repeat("│\n", paneHeight), "\n"))
	return "\n" + lipgloss.JoinHorizontal(lipgloss.Top, sidebar, divider, " ", main)
}

func (m tuiModel) renderResetSidebar(width, height int) string {
	accounts := m.resetAccounts()
	lines := []string{tuiTitleStyle.Render("Accounts"), ""}
	stride := 1
	if !m.settings.CompactMode {
		stride = 3
	}
	maxRows := max(1, (height-4)/stride)
	start, end := visibleRange(len(accounts), m.cursor, maxRows)
	for i := start; i < end; i++ {
		account := accounts[i]
		marker, nameStyle := "  ", lipgloss.NewStyle().Foreground(textColor)
		if i == m.cursor {
			marker, nameStyle = selectedRowVisual(m.tabRowFocused || m.resetRowsFocused)
		}
		if m.settings.CompactMode {
			line := marker + nameStyle.Render(account.Name) + tuiMutedStyle.Render(" · "+providerName(account.Provider))
			lines = append(lines, ansi.Truncate(line, width, "…"))
			continue
		}
		lines = append(lines, ansi.Truncate(marker+nameStyle.Render(account.Name), width, "…"))
		lines = append(lines, tuiMutedStyle.Render(ansi.Truncate("  "+providerName(account.Provider), width, "…")))
		if !m.settings.CompactMode {
			lines = append(lines, "")
		}
	}
	return strings.Join(lines, "\n")
}

func (m tuiModel) renderResetMain(width, height int) string {
	account, ok := m.resetAccount()
	if !ok {
		account, _ = m.selectedAccount()
	}
	lines := []string{tuiTitleStyle.Render(ansi.Truncate(account.Name, width, "…")), tuiMutedStyle.Render(ansi.Truncate(providerName(account.Provider), width, "…")), ""}
	if m.resetLoading && m.resetPayload == nil {
		lines = append(lines, tuiAccentStyle.Render(spinnerFrame(m.spinnerStep))+"  Loading reset credits…")
		return strings.Join(lines, "\n")
	}
	if m.resetLoading {
		lines = append(lines, tuiAccentStyle.Render(spinnerFrame(m.spinnerStep))+"  Refreshing cached resets…", "")
	}
	if m.resetErr != nil && m.resetPayload == nil {
		lines = append(lines, tuiErrorStyle.Render("Couldn’t load reset credits"), tuiMutedStyle.Render(ansi.Truncate(m.resetErr.Error(), width, "…")))
		return strings.Join(lines, "\n")
	}
	if m.resetErr != nil {
		lines = append(lines, tuiErrorStyle.Render("Refresh failed · showing cache"), "")
	}
	if m.resetPayload == nil {
		lines = append(lines, tuiMutedStyle.Render("No cached resets."), tuiMutedStyle.Render("Press Enter or → to load."))
		return strings.Join(lines, "\n")
	}
	availableColor := semanticPaletteFor(m.settings.ColorTheme).good
	if m.resetPayload.AvailableCount == 0 {
		availableColor = semanticPaletteFor(m.settings.ColorTheme).bad
	}
	available := lipgloss.NewStyle().Bold(true).Foreground(availableColor).Render(fmt.Sprintf("%d available", m.resetPayload.AvailableCount))
	lines = append(lines, available+tuiMutedStyle.Render(fmt.Sprintf(" · %d earned", m.resetPayload.TotalEarnedCount)), "")
	credits := m.resetPayload.Credits
	maxRows := max(1, (height-8)/3)
	start, end := visibleRange(len(credits), m.creditCursor, maxRows)
	for i := start; i < end; i++ {
		credit := credits[i]
		marker, titleStyle := "  ", lipgloss.NewStyle().Foreground(textColor)
		if i == m.creditCursor {
			marker, titleStyle = selectedRowVisual(m.tabRowFocused || !m.resetRowsFocused)
		}
		status := colorizeResetStatus(valueOrUnknown(credit.Status), m.settings.ColorTheme)
		title := valueOrDashString(credit.Title)
		lines = append(lines, ansi.Truncate(marker+titleStyle.Render(title)+"  "+status, width, "…"))
		timing := "  gained " + resetCreditTimeText(credit.GrantedAt, time.Now(), false) + " · expires " + resetCreditTimeText(credit.ExpiresAt, time.Now(), true)
		lines = append(lines, tuiMutedStyle.Render(ansi.Truncate(timing, width-2, "…")), "")
	}
	if len(credits) == 0 {
		lines = append(lines, tuiMutedStyle.Render("No reset credits found."))
	}
	if m.notice != "" {
		lines = append(lines, noticeStyle(m.notice).Render(ansi.Truncate(m.notice, width, "…")))
	}
	return strings.Join(lines, "\n")
}

func (m tuiModel) resetAccount() (storedAccount, bool) {
	for _, account := range m.resetAccounts() {
		if account.ID == m.resetAccountID {
			return account, true
		}
	}
	return storedAccount{}, false
}

func (m *tuiModel) showFocusedResetCache() bool {
	account, ok := m.selectedAccount()
	if !ok {
		return false
	}
	m.resetAccountID = account.ID
	m.resetPayload = m.resetCache[account.ID]
	m.resetErr = nil
	m.resetLoading = false
	m.creditCursor = 0
	m.consumeArmed, m.notice = "", ""
	if m.resetPayload != nil {
		sortResetCredits(m.resetPayload.Credits)
	}
	return m.resetPayload != nil
}

func (m tuiModel) renderAccountsTab(width, height int) string {
	if len(m.accounts) == 0 {
		return m.renderEmpty(width, "No accounts yet", "Press a to choose a provider.")
	}
	lines := []string{""}
	if width >= 80 {
		headerStyle := lipgloss.NewStyle().Foreground(mutedColor).Bold(true)
		nameW, planW, providerW := max(14, width/5), 10, 13
		emailW := max(14, width-nameW-providerW-planW-8)
		lines = append(lines, "  "+cell(headerStyle.Render("ACCOUNT NAME"), nameW)+" "+cell(headerStyle.Render("PROVIDER"), providerW)+" "+cell(headerStyle.Render("PLAN"), planW)+" "+cell(headerStyle.Render("EMAIL"), emailW), "")
		rowStride := 1
		if !m.settings.CompactMode {
			rowStride = 2
		}
		maxRows := max(1, (height-12)/rowStride)
		start, end := visibleRange(len(m.accounts), m.cursor, maxRows)
		for i := start; i < end; i++ {
			account := m.accounts[i]
			marker, nameStyle := "  ", lipgloss.NewStyle().Foreground(textColor)
			if i == m.cursor {
				marker, nameStyle = selectedRowVisual(m.tabRowFocused)
			}
			lines = append(lines, marker+cell(nameStyle.Render(account.Name), nameW)+" "+cell(tuiMutedStyle.Render(providerName(account.Provider)), providerW)+" "+cell(tuiMutedStyle.Render(accountPlan(account)), planW)+" "+cell(tuiMutedStyle.Render(valueOrDash(account.Email)), emailW))
			if !m.settings.CompactMode {
				lines = append(lines, "")
			}
		}
	} else {
		rowStride := 2
		if !m.settings.CompactMode {
			rowStride = 3
		}
		maxRows := max(1, (height-12)/rowStride)
		start, end := visibleRange(len(m.accounts), m.cursor, maxRows)
		for i := start; i < end; i++ {
			account := m.accounts[i]
			marker, nameStyle := "  ", lipgloss.NewStyle().Foreground(textColor)
			if i == m.cursor {
				marker, nameStyle = selectedRowVisual(m.tabRowFocused)
			}
			accountLine := marker + nameStyle.Render(account.Name) + "  " + tuiMutedStyle.Render(providerName(account.Provider)+" · "+accountPlan(account))
			lines = append(lines, ansi.Truncate(accountLine, width, "…"))
			lines = append(lines, "  "+tuiMutedStyle.Render(ansi.Truncate(valueOrDash(account.Email), width-2, "…")))
			if !m.settings.CompactMode {
				lines = append(lines, "")
			}
		}
	}
	if m.editingName {
		lines = append(lines, tuiBorderStyle.Width(max(20, width-4)).Render(tuiTitleStyle.Render("Rename account")+"\n\n"+tuiAccentStyle.Render("> ")+m.nameInput+"█\n\n"+tuiMutedStyle.Render("Enter save · Esc cancel")))
	} else if m.notice != "" {
		lines = append(lines, noticeStyle(m.notice).Render(ansi.Truncate(m.notice, width, "…")))
	}
	return strings.Join(lines, "\n")
}

func (m tuiModel) renderSettingsTab(width int) string {
	display := "Used"
	if m.showRemaining() {
		display = "Remaining"
	}
	items := []struct{ label, value, description string }{
		{"Usage bars", display, "Choose which side of each limit the bars represent."},
		{"Bar fill", sideText(m.settings.BarFill), "Choose which side of the track contains the filled bar."},
		{"Percentage", sideText(m.settings.PercentagePosition), "Choose which side displays the percentage."},
		{"Color palette", colorThemeText(m.settings.ColorTheme), colorThemeDescription(m.settings.ColorTheme)},
		{"Auto-refresh", refreshIntervalText(m.settings.AutoRefreshSeconds), "Refresh usage automatically after the selected interval."},
		{"Compact mode", onOffText(m.settings.CompactMode), "Remove blank rows between account entries."},
	}
	lines := []string{""}
	for i, item := range items {
		marker, labelStyle := "  ", lipgloss.NewStyle().Foreground(textColor).Bold(true)
		if i == m.settingsCursor {
			marker, labelStyle = selectedRowVisual(m.tabRowFocused)
		}
		value := tuiAccentStyle.Render("‹ " + item.value + " ›")
		gap := max(1, width-2-lipgloss.Width(item.label)-lipgloss.Width(value))
		lines = append(lines, marker+labelStyle.Render(item.label)+strings.Repeat(" ", gap)+value)
		lines = append(lines, "  "+tuiMutedStyle.Render(ansi.Truncate(item.description, width-2, "…")))
	}
	if m.notice != "" {
		lines = append(lines, noticeStyle(m.notice).Render(m.notice))
	}
	return strings.Join(lines, "\n")
}

func (m tuiModel) renderHelp(width int) string {
	key := lipgloss.NewStyle().Foreground(accentColor).Bold(true).Width(18)
	rows := []string{
		key.Render("tab") + "Focus the tab row",
		key.Render("←/→") + "Switch the focused tab",
		key.Render("↑/k  ↓/j") + "Move selection",
		key.Render("enter") + "Select or activate",
		key.Render("r") + "Refresh usage data",
		key.Render("? / esc") + "Close this help",
		key.Render("q") + "Quit",
	}
	title := tuiTitleStyle.Render("Keyboard shortcuts")
	return "\n" + tuiBorderStyle.Width(max(20, width-4)).Render(title+"\n\n"+strings.Join(rows, "\n"))
}

func (m tuiModel) renderFooter(width int) string {
	if m.authActive {
		message := "Complete sign in in your browser   esc cancel"
		if m.authSelectingProvider {
			message = "↑/↓ choose provider   enter select   esc cancel"
		} else if m.authProviderUsesAPIKey() {
			message = "type or paste API key   enter save   esc cancel"
		}
		return "\n" + tuiMutedStyle.Render(ansi.Truncate(message, width, "…"))
	}
	help := "↑/↓ navigate   q quit   tab focuses tabs   r refresh   ? help"
	switch m.tab {
	case resetsTab:
		if m.resetRowsFocused {
			help = "↑/↓ reset   q quit   enter claim/confirm   ← accounts   tab tabs"
		} else {
			help = "↑/↓ account   q quit   enter/→ open   tab focuses tabs   ? help"
		}
	case accountsTab:
		help = "↑/↓ navigate   q quit   a add provider   r reauthenticate   e rename   d remove   ? help"
	case settingsTab:
		help = "↑/↓ setting   q quit   ←/→ change   tab focuses tabs   ? help"
	}
	if m.tabRowFocused {
		help = "←/→ switch tab   q quit   ↑/↓ resume + move   tab resume content   ? help"
	}
	if m.showHelp {
		help = "? or esc close help   q quit"
	}
	return "\n" + tuiMutedStyle.Render(ansi.Truncate(help, width, "…"))
}

func (m tuiModel) showRemaining() bool { return m.settings.UsageDisplay == "remaining" }

func selectedRowVisual(tabFocused bool) (string, lipgloss.Style) {
	color := lipgloss.TerminalColor(accentColor)
	if tabFocused {
		color = mutedColor
	}
	markerStyle := lipgloss.NewStyle().Foreground(color).Bold(true)
	return markerStyle.Render("› "), lipgloss.NewStyle().Foreground(color).Bold(true)
}

func renderUsageBar(used *float64, width int, showRemaining, loading, authRequired, stale bool, barFill, percentagePosition, colorTheme string) string {
	if loading {
		skeleton := strings.Repeat("·", max(3, width-5))
		if percentagePosition == "left" {
			return tuiMutedStyle.Render("…  " + skeleton)
		}
		return tuiMutedStyle.Render(skeleton + "  …")
	}
	if authRequired {
		return tuiErrorStyle.Render(cell("sign in required", width))
	}
	if used == nil {
		return tuiMutedStyle.Render(cell("-", width))
	}
	usedValue := percentValue(*used)
	displayValue := usedValue
	if showRemaining {
		displayValue = 100 - usedValue
	}
	percentage := fmt.Sprintf("%.0f%%", displayValue)
	trackWidth := max(3, width-lipgloss.Width(percentage)-1)
	filled := int((displayValue/100)*float64(trackWidth) + 0.5)
	if displayValue > 0 && filled == 0 {
		filled = 1
	}
	barColor := usageLipglossColor(usedValue, colorTheme)
	if stale {
		barColor = semanticPaletteFor(colorTheme).warning
	}
	barStyle := lipgloss.NewStyle().Foreground(barColor)
	trackStyle := lipgloss.NewStyle().Foreground(dimTrackColor)
	filledBar := barStyle.Render(strings.Repeat("━", filled))
	emptyTrack := trackStyle.Render(strings.Repeat("─", trackWidth-filled))
	bar := filledBar + emptyTrack
	if barFill == "right" {
		bar = emptyTrack + filledBar
	}
	if percentagePosition == "left" {
		return barStyle.Render(percentage) + " " + bar
	}
	return bar + " " + barStyle.Render(percentage)
}

type semanticPalette struct {
	good    lipgloss.TerminalColor
	warning lipgloss.TerminalColor
	bad     lipgloss.TerminalColor
}

func semanticPaletteFor(theme string) semanticPalette {
	switch theme {
	case "colorblind":
		return semanticPalette{good: blueColor, warning: orangeColor, bad: magentaColor}
	case "monochrome":
		return semanticPalette{good: accentColor, warning: accentColor, bad: accentColor}
	default:
		return semanticPalette{good: greenColor, warning: amberColor, bad: redColor}
	}
}

func usageLipglossColor(used float64, theme string) lipgloss.TerminalColor {
	palette := semanticPaletteFor(theme)
	switch {
	case used >= 65:
		return palette.bad
	case used >= 50:
		return palette.warning
	default:
		return palette.good
	}
}

func renderCreditCount(summary string, stale bool, theme string) string {
	palette := semanticPaletteFor(theme)
	countText, _, _ := strings.Cut(summary, ",")
	count, err := strconv.Atoi(strings.TrimSpace(countText))
	if err != nil {
		if summary == "unavailable" {
			return lipgloss.NewStyle().Foreground(palette.bad).Render(summary)
		}
		return tuiMutedStyle.Render(summary)
	}
	style := lipgloss.NewStyle().Foreground(palette.good).Bold(true)
	if stale {
		style = style.Foreground(palette.warning)
	}
	if count == 0 {
		style = style.Foreground(palette.bad)
	}
	return style.Render(strconv.Itoa(count))
}

func colorizeResetStatus(status, theme string) string {
	palette := semanticPaletteFor(theme)
	color := palette.bad
	if status == "available" {
		color = palette.good
	} else if status == "unknown" {
		color = palette.warning
	}
	return lipgloss.NewStyle().Foreground(color).Render(status)
}

func noticeStyle(notice string) lipgloss.Style {
	if strings.Contains(strings.ToLower(notice), "couldn’t") || strings.Contains(strings.ToLower(notice), "failed") || strings.Contains(strings.ToLower(notice), "again") {
		return tuiErrorStyle
	}
	return tuiAccentStyle
}

func refreshIntervalText(seconds int) string {
	switch seconds {
	case 0:
		return "Off"
	case 30:
		return "30 seconds"
	case 60:
		return "1 minute"
	case 300:
		return "5 minutes"
	case 900:
		return "15 minutes"
	default:
		return fmt.Sprintf("%d seconds", seconds)
	}
}

func sideText(side string) string {
	if side == "left" {
		return "Left"
	}
	return "Right"
}

func colorThemeText(theme string) string {
	switch theme {
	case "colorblind":
		return "Colorblind"
	case "monochrome":
		return "Monochrome"
	default:
		return "Default"
	}
}

func colorThemeDescription(theme string) string {
	switch theme {
	case "colorblind":
		return "Use blue, orange, and magenta for semantic states."
	case "monochrome":
		return "Use the interface accent for every semantic state."
	default:
		return "Use green, amber, and red for semantic states."
	}
}

func onOffText(enabled bool) string {
	if enabled {
		return "On"
	}
	return "Off"
}

func spinnerFrame(step int) string {
	frames := []string{"◜", "◠", "◝", "◞", "◡", "◟"}
	return frames[step%len(frames)]
}

func cell(value string, width int) string {
	return lipgloss.NewStyle().Width(width).MaxWidth(width).Render(ansi.Truncate(value, width, "…"))
}

func visibleRange(total, cursor, capacity int) (int, int) {
	if total <= capacity {
		return 0, total
	}
	start := cursor - capacity/2
	start = max(0, min(start, total-capacity))
	return start, start + capacity
}

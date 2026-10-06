package main

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestCursorAccountCLIAddAndReauthenticate(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("TEST_CURSOR_KEY", "key-secret")
	path := filepath.Join(t.TempDir(), "accounts.json")
	if code := runAccountsAdd([]string{"--accounts-file", path, "--provider", "cursor", "--api-key-env", "TEST_CURSOR_KEY"}); code != 0 {
		t.Fatalf("add exited with %d", code)
	}
	store, err := loadAccounts(path)
	if err != nil {
		t.Fatal(err)
	}
	account := store.Accounts[0]
	if account.Provider != providerCursor || account.AuthData.Type != "api_key" || stringValue(account.AuthData.APIKey) != "key-secret" {
		t.Fatal("Cursor credential was stored in the wrong format")
	}
	if err := saveAccountUsageCache(account.ID, usageCacheEntry{FetchedAt: 1}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_CURSOR_KEY", "replacement-secret")
	if code := runAccountsReauth([]string{"--accounts-file", path, "--api-key-env", "TEST_CURSOR_KEY", account.ID}); code != 0 {
		t.Fatalf("reauth exited with %d", code)
	}
	store, err = loadAccounts(path)
	if err != nil || stringValue(store.Accounts[0].AuthData.APIKey) != "replacement-secret" {
		t.Fatal("session was not replaced")
	}
	if _, cached, err := loadAccountUsageCache(account.ID); err != nil || cached {
		t.Fatal("reauth did not invalidate cached usage")
	}
	for _, flags := range [][]string{{}, {"--api-key", ""}, {"--api-key", "key-secret", "--api-key-env", "TEST_CURSOR_KEY"}} {
		args := append([]string{"--accounts-file", path, "--provider", "cursor"}, flags...)
		if code := runAccountsAdd(args); code != 2 {
			t.Fatalf("invalid credential flags exited with %d", code)
		}
	}
}

func TestCursorUsageFetchAndRendering(t *testing.T) {
	client := &http.Client{Transport: usageRoundTripper(func(request *http.Request) (*http.Response, error) {
		body := `{}`
		switch request.URL.Path {
		case "/auth/exchange_user_api_key":
			body = `{"accessToken":"access-secret"}`
		case "/aiserver.v1.DashboardService/GetCurrentPeriodUsage":
			body = `{"billingCycleEnd":"1791590400000","planUsage":{"autoPercentUsed":0,"apiPercentUsed":100}}`
		case "/aiserver.v1.DashboardService/GetHardLimit":
			body = `{"hardLimit":0}`
		case "/aiserver.v1.DashboardService/GetPlanInfo":
			body = `{"planInfo":{"planName":"pro"}}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	account := storedAccount{ID: "cursor", Name: "Cursor", Provider: providerCursor, AuthData: authData{APIKey: strPtr("key-secret")}}
	result, err := fetchProviderUsage(t.Context(), client, account)
	if err != nil || !result.AccountChanged || stringValue(result.Account.PlanType) != "pro" {
		t.Fatalf("fetch = %#v, %v", result, err)
	}
	row := baseUsageRow(account)
	applyProviderUsage(&row, result.Usage)
	if isSlotBlockedByLongerWindow(row, cursorModelsSlot) || isSlotBlockedByLongerWindow(row, otherModelsSlot) {
		t.Fatal("one Cursor pool blocked the other")
	}
	if providerSupportsUsageSlot(providerCursor, sessionSlot) {
		t.Fatal("Cursor was labeled as a session window")
	}
	now := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	plain := ansi.Strip(renderTable([]usageRow{row}, now))
	for _, want := range []string{"CURSOR MODELS", "OTHER MODELS", "ON-DEMAND", "0% used / 100% left", "100% used / 0% left", "Disabled", "October 10"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("table missing %q:\n%s", want, plain)
		}
	}
	model := tuiModel{settings: defaultAppSettings(), rows: []usageRow{row}}
	for _, view := range []string{model.renderWideList(120, 24), model.renderCompactList(80, 24)} {
		for _, want := range []string{"CURSOR MODELS", "OTHER MODELS", "ON-DEMAND", "Disabled"} {
			if !strings.Contains(ansi.Strip(view), want) {
				t.Fatalf("dashboard missing %q:\n%s", want, view)
			}
		}
	}
	mixed := ansi.Strip(renderTable([]usageRow{baseUsageRow(storedAccount{Provider: providerCodex}), row}, now))
	if !strings.Contains(mixed, "SESSION (~5h)/CURSOR") || !strings.Contains(mixed, "WEEKLY/OTHER") || !strings.Contains(mixed, "Disabled") {
		t.Fatalf("mixed providers = %s", mixed)
	}
	loading := baseUsageRow(account)
	loading.Loading = true
	if usageSlotText(loading, onDemandSlot, now) != "loading…" {
		t.Fatal("missing loading state")
	}
	auth := authenticationRequiredUsageRow(account)
	if !strings.Contains(usageSlotText(auth, cursorModelsSlot, now), "Sign in") {
		t.Fatal("missing sign-in recovery message")
	}
}

func TestCursorTUIUsesBrowserLoginAndCancels(t *testing.T) {
	model := tuiModel{authActive: true, authSelectingProvider: true, authProviderID: providerCursor, authVersion: 1}
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(tuiModel)
	if command == nil || !model.authLoading || model.authSelectingProvider || model.authCursorSession == nil || model.authProviderUsesAPIKey() {
		t.Fatal("Cursor did not start browser login")
	}
	view := ansi.Strip(model.renderAuthentication(120, 24))
	if !strings.Contains(view, "cursor.com/loginDeepControl") || !strings.Contains(view, "Esc cancels") || strings.Contains(view, model.authCursorSession.Verifier) {
		t.Fatalf("incorrect browser login view: %s", view)
	}
	version := model.authVersion
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(tuiModel)
	if model.authActive || model.authCursorSession != nil || model.authCancel != nil {
		t.Fatal("login not cancelled")
	}
	updated, command = model.Update(authCompletedMsg{version: version, account: storedAccount{Provider: providerCursor}})
	if command != nil || updated.(tuiModel).authSaving {
		t.Fatal("cancelled login was saved")
	}
}

func TestCursorLoginRetainsRevocableKeyOnly(t *testing.T) {
	client := &http.Client{Transport: usageRoundTripper(func(r *http.Request) (*http.Response, error) {
		body := `{"apiKey":"key-secret"}`
		if strings.HasSuffix(r.URL.Path, "GetMe") {
			body = `{"workosId":"user-1","email":"person@example.com"}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	account, err := finishCursorLogin(t.Context(), client, "access-secret", "")
	if err != nil || account.Provider != providerCursor || stringValue(account.AuthData.APIKey) != "key-secret" || stringValue(account.AuthData.AccountID) != "user-1" || account.AuthData.RefreshToken != nil {
		t.Fatalf("login = %#v, %v", account, err)
	}
}

func TestCursorTUIBrowserAccountPersistenceAndIdentityCheck(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "accounts.json")
	existing := storedAccount{ID: "saved", Name: "Work", Provider: providerCursor, Email: strPtr("person@example.com"), AuthData: authData{Type: "api_key", APIKey: strPtr("old-key"), AccountID: strPtr("cursor-user")}}
	if err := saveAccounts(path, &accountsStore{Accounts: []storedAccount{existing}}); err != nil {
		t.Fatal(err)
	}
	if err := saveAccountUsageCache(existing.ID, usageCacheEntry{FetchedAt: 1}); err != nil {
		t.Fatal(err)
	}
	model := tuiModel{accountsPath: path, authProviderID: providerCursor, authReauthID: existing.ID}
	replacement := existing
	replacement.ID = "new"
	replacement.AuthData.APIKey = strPtr("new-key")
	message := model.saveAuthenticatedAccount(replacement)().(authSavedMsg)
	if message.err != nil {
		t.Fatal(message.err)
	}
	store, _ := loadAccounts(path)
	if len(store.Accounts) != 1 || store.Accounts[0].ID != existing.ID || store.Accounts[0].Name != "Work" || stringValue(store.Accounts[0].AuthData.APIKey) != "new-key" {
		t.Fatalf("reauth = %#v", store.Accounts)
	}
	if _, cached, _ := loadAccountUsageCache(existing.ID); cached {
		t.Fatal("old usage survived login")
	}
	replacement.AuthData.AccountID = strPtr("wrong-user")
	if message := model.saveAuthenticatedAccount(replacement)().(authSavedMsg); message.err == nil {
		t.Fatal("wrong identity replaced account")
	}
	existing.Email = nil
	replacement.Email = nil
	replacement.AuthData.AccountID = existing.AuthData.AccountID
	if findMatchingAccount([]storedAccount{existing}, replacement) != 0 {
		t.Fatal("missing email caused duplicate Cursor account")
	}
}

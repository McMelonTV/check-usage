package tui

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/internal/storage"
	"github.com/McMelonTV/check-usage/internal/usage"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestCursorUsageFetchAndRendering(t *testing.T) {
	client := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		body := `{}`
		switch request.URL.Path {
		case "/auth/exchange_user_api_key":
			body = `{"accessToken":"access-secret"}`
		case "/aiserver.v1.DashboardService/GetCurrentPeriodUsage":
			body = `{"billingCycleEnd":"1791590400000","planUsage":{"autoPercentUsed":0,"apiPercentUsed":100}}`
		case "/aiserver.v1.DashboardService/GetPlanInfo":
			body = `{"planInfo":{"planName":"pro"}}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	account := storage.Account{ID: "cursor", Name: "Cursor", Provider: providers.Cursor, AuthData: storage.AuthData{APIKey: new("key-secret")}}
	result, err := usage.FetchProviderUsage(t.Context(), client, account)
	if err != nil || !result.AccountChanged || storage.StringValue(result.Account.PlanType) != "pro" {
		t.Fatalf("fetch = %#v, %v", result, err)
	}
	row := usage.BaseRow(account)
	usage.ApplyProviderUsage(&row, result.Usage)
	if usage.IsSlotBlockedByLongerWindow(row, providers.WeeklySlot) || usage.IsSlotBlockedByLongerWindow(row, providers.MonthlySlot) {
		t.Fatal("one Cursor pool blocked the other")
	}
	for _, metric := range row.Metrics {
		if !metric.IsScoped() {
			t.Fatalf("Cursor metric %#v is not scoped, so it would read as a session/weekly/monthly window", metric)
		}
	}
	now := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	plain := ansi.Strip(usage.RenderTable([]usage.Row{row}, now))
	for _, want := range []string{"SESSION (~5h)", "✦ 0% used / 100% left", "✦ 100% used / 0% left", "Cursor models · ", "Other models · "} {
		if !strings.Contains(plain, want) {
			t.Fatalf("table missing %q:\n%s", want, plain)
		}
	}
	model := Model{settings: storage.DefaultSettings(), rows: []usage.Row{row}}
	wide, compact := ansi.Strip(model.renderWideList(120, 24)), ansi.Strip(model.renderCompactList(80, 24))
	if strings.Contains(wide, "SESSION (~5h) ✦") || strings.Contains(plain, "Disabled") || strings.Contains(wide, "USD") {
		t.Fatalf("Cursor still uses the session column or shows spend:\n%s", wide)
	}
	if usage.SlotText(row, providers.SessionSlot, now) != "-" || !strings.Contains(usage.SlotLabel(row, providers.WeeklySlot), "CURSOR") || !strings.Contains(usage.SlotLabel(row, providers.MonthlySlot), "OTHER") {
		t.Fatalf("Cursor pools are in the wrong columns: %#v", row.Metrics)
	}
	for _, want := range []string{"WEEKLY ✦", "MONTHLY ✦", "Cursor models · ", "Other models · "} {
		if !strings.Contains(wide, want) {
			t.Fatalf("dashboard missing %q:\n%s", want, wide)
		}
	}
	for _, want := range []string{"CURSOR MODELS", "OTHER MODELS"} {
		if !strings.Contains(compact, want) {
			t.Fatalf("compact dashboard missing %q:\n%s", want, compact)
		}
	}
	for _, text := range []string{wide, ansi.Strip(usage.RenderTable([]usage.Row{usage.BaseRow(storage.Account{Provider: providers.Codex}), row}, now))} {
		if strings.Contains(text, "/CURSOR") || strings.Contains(text, "/OTHER") || strings.Contains(text, "/SPEND") {
			t.Fatalf("combined column names are back:\n%s", text)
		}
	}
	loading := usage.BaseRow(account)
	loading.Loading = true
	if usage.SlotText(loading, providers.WeeklySlot, now) != "loading…" || usage.SlotText(loading, providers.MonthlySlot, now) != "loading…" || usage.SlotText(loading, providers.SessionSlot, now) != "-" {
		t.Fatal("missing loading state")
	}
	auth := usage.AuthenticationRequiredRow(account)
	if !strings.Contains(usage.SlotText(auth, providers.SessionSlot, now), "Sign in") {
		t.Fatal("missing sign-in recovery message")
	}
}

func TestCursorTUIUsesBrowserLoginAndCancels(t *testing.T) {
	model := Model{authActive: true, authSelectingProvider: true, authProviderID: providers.Cursor, authVersion: 1}
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if command == nil || !model.authLoading || model.authSelectingProvider || model.authCursorSession == nil || model.authProviderUsesAPIKey() {
		t.Fatal("Cursor did not start browser login")
	}
	model.width, model.height = 120, 24
	view := ansi.Strip(model.View())
	if !strings.Contains(view, "cursor.com/loginDeepControl") || !strings.Contains(view, "esc cancel") || strings.Contains(view, model.authCursorSession.Verifier) {
		t.Fatalf("incorrect browser login view: %s", view)
	}
	version := model.authVersion
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(Model)
	if model.authActive || model.authCursorSession != nil || model.authCancel != nil {
		t.Fatal("login not cancelled")
	}
	updated, command = model.Update(authCompletedMsg{version: version, account: storage.Account{Provider: providers.Cursor}})
	if command != nil || updated.(Model).authSaving {
		t.Fatal("cancelled login was saved")
	}
}

func TestCursorTUIBrowserAccountPersistenceAndIdentityCheck(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "accounts.json")
	existing := storage.Account{ID: "saved", Name: "Work", Provider: providers.Cursor, Email: new("person@example.com"), AuthData: storage.AuthData{Type: "api_key", APIKey: new("old-key"), AccountID: new("cursor-user")}}
	if err := storage.SaveAccounts(path, &storage.AccountStore{Accounts: []storage.Account{existing}}); err != nil {
		t.Fatal(err)
	}
	if err := storage.SaveAccountCache(existing.ID, storage.CacheEntry{FetchedAt: 1}); err != nil {
		t.Fatal(err)
	}
	model := Model{accountsPath: path, authProviderID: providers.Cursor, authReauthID: existing.ID}
	replacement := existing
	replacement.ID = "new"
	replacement.AuthData.APIKey = new("new-key")
	message := model.saveAuthenticatedAccount(replacement)().(authSavedMsg)
	if message.err != nil {
		t.Fatal(message.err)
	}
	store, _ := storage.LoadAccounts(path)
	if len(store.Accounts) != 1 || store.Accounts[0].ID != existing.ID || store.Accounts[0].Name != "Work" || storage.StringValue(store.Accounts[0].AuthData.APIKey) != "new-key" {
		t.Fatalf("reauth = %#v", store.Accounts)
	}
	if _, cached, _ := storage.LoadAccountCache(existing.ID); cached {
		t.Fatal("old usage survived login")
	}
	replacement.AuthData.AccountID = new("wrong-user")
	if message := model.saveAuthenticatedAccount(replacement)().(authSavedMsg); message.err == nil {
		t.Fatal("wrong identity replaced account")
	}
	existing.Email = nil
	replacement.Email = nil
	replacement.AuthData.AccountID = existing.AuthData.AccountID
	if storage.FindMatchingAccount([]storage.Account{existing}, replacement) != 0 {
		t.Fatal("missing email caused duplicate Cursor account")
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

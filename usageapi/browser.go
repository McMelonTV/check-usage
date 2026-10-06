package usageapi

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/McMelonTV/check-usage/internal/providers/cursorapi"
)

// BeginBrowserAuth starts Cursor's SDK browser handshake without opening a UI.
func (service *Service) BeginBrowserAuth(ctx context.Context, provider string) (BrowserAuthSession, error) {
	if provider != providerCursor {
		return BrowserAuthSession{}, fmt.Errorf("provider %q does not support browser authentication", provider)
	}
	if err := ctx.Err(); err != nil {
		return BrowserAuthSession{}, err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	now := service.now()
	for id, session := range service.browserSessions {
		if !now.Before(session.ExpiresAt) {
			delete(service.browserSessions, id)
		}
	}
	session, err := cursorapi.BeginLogin(now)
	if err != nil {
		return BrowserAuthSession{}, err
	}
	service.browserSessions[session.ID] = session
	return BrowserAuthSession{Provider: provider, SessionID: session.ID, VerificationURL: session.LoginURL, PollIntervalSeconds: int(cursorapi.PollInterval.Seconds())}, nil
}

// PollBrowserAuth polls once, then saves an SDK-style API key after approval.
// Session verifiers never leave this Service or appear in public results.
func (service *Service) PollBrowserAuth(ctx context.Context, request BrowserAuthPoll) (DeviceAuthResult, error) {
	if request.Provider != providerCursor {
		return DeviceAuthResult{}, fmt.Errorf("provider %q does not support browser authentication", request.Provider)
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	session, ok := service.browserSessions[request.SessionID]
	if !ok {
		return DeviceAuthResult{}, fmt.Errorf("unknown browser login; begin login on this service instance")
	}
	if !service.now().Before(session.ExpiresAt) {
		delete(service.browserSessions, session.ID)
		return DeviceAuthResult{}, fmt.Errorf("Cursor browser login expired; start login again")
	}
	token, pending, err := cursorapi.PollLogin(ctx, service.client, session, service.now())
	if err != nil {
		return DeviceAuthResult{}, err
	}
	if pending {
		return DeviceAuthResult{Status: "pending"}, nil
	}
	delete(service.browserSessions, session.ID)
	identity, err := cursorapi.GetIdentity(ctx, service.client, token)
	if err != nil {
		return DeviceAuthResult{}, err
	}
	store, err := service.loadAccounts()
	if err != nil {
		return DeviceAuthResult{}, err
	}
	index := -1
	if strings.TrimSpace(request.Account) != "" {
		index, err = findAccount(store.Accounts, request.Account)
		if err != nil {
			return DeviceAuthResult{}, err
		}
		existing := store.Accounts[index]
		if existing.Provider != providerCursor {
			return DeviceAuthResult{}, fmt.Errorf("account %q does not use Cursor", existing.Name)
		}
		if id := stringValue(existing.AuthData.AccountID); id != "" && id != identity.AccountID() {
			return DeviceAuthResult{}, fmt.Errorf("signed in to a different Cursor account")
		}
		if email := stringValue(existing.Email); email != "" && !strings.EqualFold(email, identity.Email) {
			return DeviceAuthResult{}, fmt.Errorf("signed in to a different Cursor account")
		}
	}
	key, err := cursorapi.CreateAPIKey(ctx, service.client, token, service.now())
	if err != nil {
		return DeviceAuthResult{}, err
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		name = defaultAccountName(identity.Email, service.now())
	}
	candidate := storedAccount{ID: newAccountID(service.now()), Name: name, Provider: providerCursor, Email: stringPointer(identity.Email), AuthData: authData{Type: string(apiKeyCredentials), APIKey: &key, AccessToken: &token, AccountID: stringPointer(identity.AccountID())}}
	if index < 0 {
		index = matchingLogin(store.Accounts, candidate, strings.TrimSpace(request.Name) != "")
	}
	action := "added"
	if index >= 0 {
		candidate.ID, candidate.PlanType = store.Accounts[index].ID, store.Accounts[index].PlanType
		if strings.TrimSpace(request.Name) == "" {
			candidate.Name = store.Accounts[index].Name
		}
		store.Accounts[index] = candidate
		action = "updated"
	} else {
		store.Accounts = append(store.Accounts, candidate)
	}
	if err := service.saveAccounts(store); err != nil {
		return DeviceAuthResult{}, err
	}
	if err := service.clearCache(candidate.ID); err != nil {
		return DeviceAuthResult{}, err
	}
	account := candidate.public()
	return DeviceAuthResult{Status: "complete", Action: action, Account: &account}, nil
}

func (service *Service) clearCache(id string) error {
	if err := os.Remove(service.cachePath(id)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

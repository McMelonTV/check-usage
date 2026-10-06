package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/McMelonTV/check-usage/cursorapi"
)

func runCursorBrowserLogin(name string, client *http.Client, browser bool) (storedAccount, error) {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	session, err := cursorapi.BeginLogin(time.Now())
	if err != nil {
		return storedAccount{}, err
	}
	fmt.Printf("Sign in to Cursor in your browser:\n%s\n", session.LoginURL)
	if browser {
		_ = openBrowserURL(session.LoginURL)
	}
	return waitForCursorLogin(ctx, client, session, name)
}

func waitForCursorLogin(ctx context.Context, client *http.Client, session cursorapi.LoginSession, name string) (storedAccount, error) {
	for {
		token, pending, err := cursorapi.PollLogin(ctx, client, session, time.Now())
		if err != nil {
			return storedAccount{}, err
		}
		if !pending {
			return finishCursorLogin(ctx, client, token, name)
		}
		timer := time.NewTimer(cursorapi.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return storedAccount{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func finishCursorLogin(ctx context.Context, client *http.Client, token, name string) (storedAccount, error) {
	identity, err := cursorapi.GetIdentity(ctx, client, token)
	if err != nil {
		return storedAccount{}, err
	}
	key, err := cursorapi.CreateAPIKey(ctx, client, token, time.Now())
	if err != nil {
		return storedAccount{}, err
	}
	if strings.TrimSpace(name) == "" {
		name = firstNonEmpty(identity.Email, "Cursor")
	}
	return storedAccount{ID: newAccountID(), Name: name, Provider: providerCursor, Email: optionalString(identity.Email), AuthData: authData{Type: string(apiKeyCredentials), APIKey: strPtr(key), AccessToken: strPtr(token), AccountID: strPtr(identity.AccountID())}}, nil
}

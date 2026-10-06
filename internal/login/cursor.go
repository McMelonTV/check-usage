package login

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/McMelonTV/check-usage/internal/cursorapi"
	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/internal/storage"
	"github.com/McMelonTV/check-usage/internal/usage"
)

func RunCursorBrowser(name string, client *http.Client, browser bool) (storage.Account, error) {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	session, err := cursorapi.BeginLogin(time.Now())
	if err != nil {
		return storage.Account{}, err
	}
	printSignInLink("Cursor", session.LoginURL, browser)
	fmt.Println("Waiting for approval…")
	return WaitForCursor(ctx, client, session, name)
}

func WaitForCursor(ctx context.Context, client *http.Client, session cursorapi.LoginSession, name string) (storage.Account, error) {
	for {
		token, pending, err := cursorapi.PollLogin(ctx, client, session, time.Now())
		if err != nil {
			return storage.Account{}, err
		}
		if !pending {
			return FinishCursor(ctx, client, token, name)
		}
		timer := time.NewTimer(cursorapi.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return storage.Account{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func FinishCursor(ctx context.Context, client *http.Client, token, name string) (storage.Account, error) {
	identity, err := cursorapi.GetIdentity(ctx, client, token)
	if err != nil {
		return storage.Account{}, err
	}
	key, err := cursorapi.CreateAPIKey(ctx, client, token, time.Now())
	if err != nil {
		return storage.Account{}, err
	}
	if strings.TrimSpace(name) == "" {
		name = usage.FirstNonEmpty(identity.Email, "Cursor")
	}
	return storage.Account{ID: storage.NewAccountID(), Name: name, Provider: providers.Cursor, Email: storage.OptionalString(identity.Email), AuthData: storage.AuthData{Type: string(providers.APIKey), APIKey: new(key), AccessToken: new(token), AccountID: new(identity.AccountID())}}, nil
}

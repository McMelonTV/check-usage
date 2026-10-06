package login

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/McMelonTV/check-usage/internal/claudeapi"
	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/internal/storage"
)

func RunClaude(accountName string, client *http.Client, openBrowser bool) (storage.Account, error) {
	session, err := claudeapi.NewAuthSession()
	if err != nil {
		return storage.Account{}, err
	}
	printSignInLink("Claude", session.URL, openBrowser)
	fmt.Print("Paste the authorization code shown after signing in: ")
	code, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && (err != io.EOF || strings.TrimSpace(code) == "") {
		return storage.Account{}, fmt.Errorf("read authorization code: %w", err)
	}
	return CompleteClaude(client, session, accountName, code)
}

func CompleteClaude(client *http.Client, session claudeapi.AuthSession, accountName, code string) (storage.Account, error) {
	login, err := claudeapi.CompleteLogin(context.Background(), client, session, code, claudeapi.DefaultUserAgent, time.Now())
	if err != nil {
		return storage.Account{}, err
	}
	return buildClaudeAccount(accountName, login), nil
}

func buildClaudeAccount(requestedName string, login claudeapi.Login) storage.Account {
	email := storage.OptionalString(login.Email)
	name := strings.TrimSpace(requestedName)
	if name == "" {
		name = defaultAccountName(email)
	}
	account := storage.Account{
		ID:       storage.NewAccountID(),
		Name:     name,
		Provider: providers.Claude,
		Email:    email,
		PlanType: storage.OptionalString(login.Plan),
		AuthData: storage.AuthData{Type: "claude_oauth", AccountID: storage.OptionalString(login.AccountUUID)},
	}
	storage.SetClaudeCredentials(&account, login.Credentials)
	return account
}

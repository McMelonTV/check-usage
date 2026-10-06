package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/McMelonTV/check-usage/claudeapi"
)

func runClaudeLogin(accountName string, client *http.Client, openBrowser bool) (storedAccount, error) {
	session, err := claudeapi.NewAuthSession()
	if err != nil {
		return storedAccount{}, err
	}
	printSignInLink("Claude", session.URL, openBrowser)
	fmt.Print("Paste the authorization code shown after signing in: ")
	code, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && (err != io.EOF || strings.TrimSpace(code) == "") {
		return storedAccount{}, fmt.Errorf("read authorization code: %w", err)
	}
	return completeClaudeLogin(client, session, accountName, code)
}

func completeClaudeLogin(client *http.Client, session claudeapi.AuthSession, accountName, code string) (storedAccount, error) {
	login, err := claudeapi.CompleteLogin(context.Background(), client, session, code, claudeapi.DefaultUserAgent, time.Now())
	if err != nil {
		return storedAccount{}, err
	}
	return buildClaudeAccount(accountName, login), nil
}

func buildClaudeAccount(requestedName string, login claudeapi.Login) storedAccount {
	email := optionalString(login.Email)
	name := strings.TrimSpace(requestedName)
	if name == "" {
		name = defaultAccountName(email)
	}
	account := storedAccount{
		ID:       newAccountID(),
		Name:     name,
		Provider: providerClaude,
		Email:    email,
		PlanType: optionalString(login.Plan),
		AuthData: authData{Type: "claude_oauth", AccountID: optionalString(login.AccountUUID)},
	}
	setClaudeCredentials(&account, login.Credentials)
	return account
}

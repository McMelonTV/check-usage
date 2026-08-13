package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/McMelonTV/check-usage/codexapi"
)

var errMissingCredentials = errors.New("missing access/refresh token")

func ensureFreshTokens(acc storedAccount, client *http.Client) (storedAccount, bool, error) {
	if acc.AuthData.AccessToken == nil || acc.AuthData.RefreshToken == nil {
		return acc, false, errMissingCredentials
	}
	credentials := codexapi.Credentials{AccessToken: *acc.AuthData.AccessToken, RefreshToken: *acc.AuthData.RefreshToken}
	if acc.AuthData.IDToken != nil {
		credentials.IDToken = *acc.AuthData.IDToken
	}
	if acc.AuthData.AccountID != nil {
		credentials.AccountID = *acc.AuthData.AccountID
	}
	updated, changed, err := codexapi.RefreshCredentials(context.Background(), client, credentials, time.Now())
	if err != nil {
		return acc, false, err
	}
	if !changed {
		return acc, false, nil
	}
	acc.AuthData.AccessToken = strPtr(updated.AccessToken)
	acc.AuthData.RefreshToken = strPtr(updated.RefreshToken)
	if updated.IDToken != "" {
		acc.AuthData.IDToken = strPtr(updated.IDToken)
	}
	return acc, true, nil
}

func authenticationRequired(err error) bool {
	return errors.Is(err, errMissingCredentials) || codexapi.IsAuthenticationError(err)
}

func fetchUsage(acc storedAccount, client *http.Client) (*rateLimitStatusPayload, error) {
	if acc.AuthData.AccessToken == nil {
		return nil, fmt.Errorf("missing access token")
	}
	return codexapi.FetchUsage(context.Background(), client, *acc.AuthData.AccessToken, stringValue(acc.AuthData.AccountID), userAgent)
}

func fetchResetCredits(acc storedAccount, client *http.Client) (*resetCreditsPayload, error) {
	if acc.AuthData.AccessToken == nil {
		return nil, fmt.Errorf("missing access token")
	}
	return codexapi.FetchResetCredits(context.Background(), client, *acc.AuthData.AccessToken, stringValue(acc.AuthData.AccountID), userAgent)
}

func tokenExpiredOrNear(token string) bool {
	return codexapi.TokenExpiredOrNear(token, time.Now(), time.Duration(expirySkew)*time.Second)
}

func jwtExp(token string) (int64, bool) {
	return codexapi.JWTExpiry(token)
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

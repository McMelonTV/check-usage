package usage

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/McMelonTV/check-usage/internal/claudeapi"
	"github.com/McMelonTV/check-usage/internal/codexapi"
	"github.com/McMelonTV/check-usage/internal/storage"
)

var errMissingCredentials = errors.New("missing access/refresh token")

func EnsureFreshTokens(acc storage.Account, client *http.Client) (storage.Account, bool, error) {
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
	acc.AuthData.AccessToken = new(updated.AccessToken)
	acc.AuthData.RefreshToken = new(updated.RefreshToken)
	if updated.IDToken != "" {
		acc.AuthData.IDToken = new(updated.IDToken)
	}
	return acc, true, nil
}

func authenticationRequired(err error) bool {
	return errors.Is(err, errMissingCredentials) || codexapi.IsAuthenticationError(err) || claudeapi.IsAuthenticationError(err)
}

func FetchUsage(acc storage.Account, client *http.Client) (*codexapi.RateLimitStatusPayload, error) {
	if acc.AuthData.AccessToken == nil {
		return nil, fmt.Errorf("missing access token")
	}
	return codexapi.FetchUsage(context.Background(), client, *acc.AuthData.AccessToken, storage.StringValue(acc.AuthData.AccountID), UserAgent)
}

func FetchResetCredits(acc storage.Account, client *http.Client) (*codexapi.ResetCreditsPayload, error) {
	if acc.AuthData.AccessToken == nil {
		return nil, fmt.Errorf("missing access token")
	}
	return codexapi.FetchResetCredits(context.Background(), client, *acc.AuthData.AccessToken, storage.StringValue(acc.AuthData.AccountID), UserAgent)
}

func tokenExpiredOrNear(token string) bool {
	return codexapi.TokenExpiredOrNear(token, time.Now(), time.Duration(expirySkew)*time.Second)
}

func jwtExp(token string) (int64, bool) {
	return codexapi.JWTExpiry(token)
}

const (
	expirySkew = 60
	UserAgent  = "codex-cli/1.0.0"
)

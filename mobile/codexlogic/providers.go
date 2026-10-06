package codexlogic

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/internal/providers/claudeapi"
	"github.com/McMelonTV/check-usage/internal/providers/cursorapi"
)

// ProviderCredentials is passed only across the local encrypted Android bridge.
type ProviderCredentials struct {
	AccessToken     string `json:"accessToken"`
	RefreshToken    string `json:"refreshToken"`
	IDToken         string `json:"idToken"`
	RemoteAccountID string `json:"remoteAccountId,omitempty"`
	APIKey          string `json:"apiKey"`
	ExpiresAt       int64  `json:"expiresAt"`
	PlanCheckedAt   int64  `json:"planCheckedAt"`
}
type loginResult struct {
	Credentials ProviderCredentials `json:"credentials"`
	Email       string              `json:"email,omitempty"`
	Plan        string              `json:"plan,omitempty"`
}
type browserSession struct {
	SessionID string                  `json:"sessionId"`
	URL       string                  `json:"verificationUrl"`
	Cursor    *cursorapi.LoginSession `json:"cursor,omitempty"`
}

// BeginProviderLogin starts Claude's pasted-code or Cursor's browser flow.
func BeginProviderLogin(provider string) (string, error) {
	switch provider {
	case providers.Claude:
		session, err := claudeapi.NewAuthSession()
		return encode(browserSession{SessionID: session.SessionID(), URL: session.URL}, err)
	case providers.Cursor:
		session, err := cursorapi.BeginLogin(time.Now())
		return encode(browserSession{SessionID: session.ID, URL: session.LoginURL, Cursor: &session}, err)
	default:
		return "", fmt.Errorf("provider %q does not support browser login", provider)
	}
}

// CompleteProviderLogin returns empty while Cursor authorization is pending.
func CompleteProviderLogin(provider, sessionJSON, code string) (string, error) {
	var session browserSession
	if err := json.Unmarshal([]byte(sessionJSON), &session); err != nil {
		return "", err
	}
	ctx, now := context.Background(), time.Now()
	switch provider {
	case providers.Claude:
		auth, err := claudeapi.ParseSessionID(session.SessionID)
		if err != nil {
			return "", err
		}
		login, err := claudeapi.CompleteLogin(ctx, client, auth, code, now)
		return encode(loginResult{Credentials: ProviderCredentials{AccessToken: login.Credentials.AccessToken, RefreshToken: login.Credentials.RefreshToken, ExpiresAt: login.Credentials.ExpiresAt, RemoteAccountID: login.AccountUUID}, Email: login.Email, Plan: login.Plan}, providerError(err))
	case providers.Cursor:
		if session.Cursor == nil {
			return "", fmt.Errorf("missing Cursor login session")
		}
		token, pending, err := cursorapi.PollLogin(ctx, client, *session.Cursor, now)
		if err != nil || pending {
			return "", providerError(err)
		}
		identity, err := cursorapi.GetIdentity(ctx, client, token)
		if err != nil {
			return "", providerError(err)
		}
		key, err := cursorapi.CreateAPIKey(ctx, client, token, now)
		return encode(loginResult{Credentials: ProviderCredentials{AccessToken: token, APIKey: key, RemoteAccountID: identity.AccountID()}, Email: identity.Email}, providerError(err))
	default:
		return "", fmt.Errorf("unknown browser provider: %q", provider)
	}
}

type providerSnapshot struct {
	Credentials ProviderCredentials `json:"credentials"`
	Usage       providers.Usage     `json:"usage"`
	FetchedAt   int64               `json:"fetchedAt"`
	Error       string              `json:"error,omitempty"`
	RetryAt     int64               `json:"retryAt,omitempty"`
}

// FetchProviderUsage shares all metric mapping and token renewal with the TUI.
func FetchProviderUsage(provider, credentialsJSON string) (string, error) {
	var credentials ProviderCredentials
	if err := json.Unmarshal([]byte(credentialsJSON), &credentials); err != nil {
		return "", err
	}
	ctx, now := context.Background(), time.Now()
	result := providerSnapshot{Credentials: credentials, FetchedAt: now.UnixMilli()}
	var err error
	switch provider {
	case providers.Claude:
		var fetched providers.ClaudeResult
		fetched, err = providers.FetchClaudeUsage(ctx, client, claudeapi.Credentials{AccessToken: credentials.AccessToken, RefreshToken: credentials.RefreshToken, ExpiresAt: credentials.ExpiresAt}, now, credentials.PlanCheckedAt == 0 || now.Unix()-credentials.PlanCheckedAt >= int64(providers.ClaudePlanCheckInterval.Seconds()))
		if fetched.Credentials.AccessToken != "" {
			result.Credentials.AccessToken, result.Credentials.RefreshToken, result.Credentials.ExpiresAt = fetched.Credentials.AccessToken, fetched.Credentials.RefreshToken, fetched.Credentials.ExpiresAt
		}
		result.Usage = fetched.Usage
		if fetched.PlanChecked {
			result.Credentials.PlanCheckedAt = now.Unix()
		}
		if delay, limited := claudeapi.RateLimitDelay(err); limited {
			result.RetryAt = now.Add(delay).UnixMilli()
		} else {
			result.RetryAt = now.Add(claudeapi.MinRefreshInterval).UnixMilli()
		}
	case providers.Cursor:
		result.Usage, result.Credentials.AccessToken, err = providers.FetchCursorUsage(ctx, client, credentials.APIKey, credentials.AccessToken, now)
	case providers.OpenCodeGo, providers.DeepSeek:
		result.Usage, err = providers.FetchAPIKeyUsage(ctx, client, provider, credentials.APIKey, "usage-widgets/0.1.0")
	default:
		return "", fmt.Errorf("unknown usage provider: %q", provider)
	}
	// Empty metric lists must encode as [] rather than null for Kotlin callers.
	if result.Usage.Metrics == nil {
		result.Usage.Metrics = []providers.Metric{}
	}
	// Persist renewed tokens even when the usage endpoint fails.
	if err != nil {
		result.Error = providerError(err).Error()
	}
	return encode(result, nil)
}

func providerError(err error) error {
	if err == nil {
		return nil
	}
	if providers.IsCredentialError(err) || claudeapi.IsAuthenticationError(err) || cursorapi.IsAuthenticationError(err) {
		return fmt.Errorf("authentication required: %w", err)
	}
	return err
}

// ValidateAPIKey refuses unsupported providers and empty credentials before storage.
func ValidateAPIKey(provider, key string) error {
	definition, ok := providers.Get(provider)
	if !ok || definition.Credentials != providers.APIKey {
		return fmt.Errorf("unsupported API-key provider: %q", provider)
	}
	if strings.TrimSpace(key) == "" {
		return providers.ErrMissingAPIKey
	}
	return nil
}

package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/McMelonTV/check-usage/claudeapi"
	"github.com/McMelonTV/check-usage/providers"
)

const (
	providerCodex      = providers.Codex
	providerOpenCodeGo = providers.OpenCodeGo
	providerDeepSeek   = providers.DeepSeek
	providerClaude     = providers.Claude
	providerCursor     = providers.Cursor
)

type credentialMode = providers.CredentialMode
type providerMetric = providers.Metric
type providerUsage = providers.Usage
type metricKind = providers.MetricKind
type metricSlot = providers.MetricSlot

const (
	deviceCredentials    = providers.Device
	apiKeyCredentials    = providers.APIKey
	oauthCodeCredentials = providers.OAuthCode
	percentageMetric     = providers.Percentage
	sessionSlot          = providers.SessionSlot
	weeklySlot           = providers.WeeklySlot
	monthlySlot          = providers.MonthlySlot
	textMetric           = providers.Text
)

type providerDefinition struct {
	ID           string
	Name         string
	Plan         string
	Credentials  credentialMode
	ResetCredits bool
}

type providerFetchResult struct {
	Usage          providerUsage
	Account        storedAccount
	AccountChanged bool
	ResetCredits   *resetCreditsPayload
	ResetError     error
	RateLimit      *rateLimitDetails
	// NextFetchAt, when set, is the earliest time to ask the provider again.
	NextFetchAt time.Time
	// PlanCheckedAt, when set, is when the plan was read from the provider.
	PlanCheckedAt time.Time
}

func providerDefinitions() []providerDefinition {
	definitions := providers.Definitions()
	result := make([]providerDefinition, 0, len(definitions))
	for _, definition := range definitions {
		result = append(result, providerDefinition{ID: definition.ID, Name: definition.Name, Plan: definition.Plan, Credentials: definition.Credentials, ResetCredits: definition.SupportsResetCredits})
	}
	return result
}

func providerFor(id string) (providerDefinition, error) {
	definition, ok := providers.Get(id)
	if !ok {
		return providerDefinition{}, fmt.Errorf("unsupported provider %q", id)
	}
	return providerDefinition{ID: definition.ID, Name: definition.Name, Plan: definition.Plan, Credentials: definition.Credentials, ResetCredits: definition.SupportsResetCredits}, nil
}

func providerName(id string) string {
	definition, err := providerFor(id)
	if err != nil {
		return id
	}
	return definition.Name
}

func accountPlan(account storedAccount) string {
	if account.PlanType != nil && *account.PlanType != "" {
		return *account.PlanType
	}
	provider, err := providerFor(account.Provider)
	if err != nil {
		return "-"
	}
	return firstNonEmpty(provider.Plan, "-")
}

func emptyProviderMetrics(providerID string) []providerMetric {
	switch providerID {
	case providerCodex, providerClaude:
		return []providerMetric{{Kind: percentageMetric, Slot: sessionSlot, Label: "SESSION"}, {Kind: percentageMetric, Slot: weeklySlot, Label: "WEEKLY"}}
	case providerOpenCodeGo:
		return []providerMetric{{Kind: percentageMetric, Slot: sessionSlot, Label: "SESSION"}, {Kind: percentageMetric, Slot: weeklySlot, Label: "WEEKLY"}, {Kind: percentageMetric, Slot: monthlySlot, Label: "MONTHLY"}}
	case providerDeepSeek:
		return nil
	case providerCursor:
		return providers.CursorMetrics()
	default:
		return nil
	}
}

func fetchProviderUsage(ctx context.Context, client *http.Client, account storedAccount) (providerFetchResult, error) {
	definition, err := providerFor(account.Provider)
	if err != nil {
		return providerFetchResult{}, err
	}
	if definition.Credentials == apiKeyCredentials {
		var usage providerUsage
		changed := false
		if definition.ID == providerCursor {
			var token string
			usage, token, err = providers.FetchCursorUsage(ctx, client, stringValue(account.AuthData.APIKey), stringValue(account.AuthData.AccessToken), time.Now())
			changed = err == nil && token != stringValue(account.AuthData.AccessToken)
			if changed {
				account.AuthData.AccessToken = strPtr(token)
			}
		} else {
			usage, err = providers.FetchAPIKeyUsage(ctx, client, definition.ID, stringValue(account.AuthData.APIKey), userAgent)
		}
		if err != nil {
			return providerFetchResult{}, err
		}
		if usage.Plan != "" && stringValue(account.PlanType) != usage.Plan {
			account.PlanType = strPtr(usage.Plan)
			changed = true
		}
		return providerFetchResult{Usage: usage, Account: account, AccountChanged: changed}, nil
	}
	if definition.ID == providerClaude {
		return fetchClaudeUsage(ctx, client, account)
	}
	return fetchCodexUsage(client, account)
}

func fetchClaudeUsage(ctx context.Context, client *http.Client, account storedAccount) (providerFetchResult, error) {
	credentials := claudeapi.Credentials{AccessToken: stringValue(account.AuthData.AccessToken), RefreshToken: stringValue(account.AuthData.RefreshToken)}
	if account.AuthData.ExpiresAt != nil {
		credentials.ExpiresAt = *account.AuthData.ExpiresAt
	}
	now := time.Now()
	var planCheckedAt time.Time
	if entry, ok, err := loadAccountUsageCache(account.ID); err == nil && ok && entry.PlanCheckedAt > 0 {
		planCheckedAt = time.Unix(entry.PlanCheckedAt, 0)
	}
	fetchPlan := providers.ClaudePlanDue(stringValue(account.PlanType), planCheckedAt, now)
	result, err := providers.FetchClaudeUsage(ctx, client, credentials, claudeapi.DefaultUserAgent, now, fetchPlan)
	changed := result.CredentialsChanged
	if changed {
		setClaudeCredentials(&account, result.Credentials)
	}
	if err != nil {
		// Keep rotated tokens even when the usage request itself fails.
		fetch := providerFetchResult{Account: account, AccountChanged: changed}
		if delay, limited := claudeapi.RateLimitDelay(err); limited {
			fetch.NextFetchAt = now.Add(delay)
		}
		return fetch, err
	}
	if result.Usage.Plan != "" && stringValue(account.PlanType) != result.Usage.Plan {
		account.PlanType = strPtr(result.Usage.Plan)
		changed = true
	}
	// Keep the account's plan when the profile was not requested this time.
	result.Usage.Plan = firstNonEmpty(result.Usage.Plan, stringValue(account.PlanType))
	fetch := providerFetchResult{Usage: result.Usage, Account: account, AccountChanged: changed, NextFetchAt: now.Add(claudeapi.MinRefreshInterval)}
	if result.PlanChecked {
		fetch.PlanCheckedAt = now
	}
	return fetch, nil
}

func setClaudeCredentials(account *storedAccount, credentials claudeapi.Credentials) {
	account.AuthData.AccessToken = strPtr(credentials.AccessToken)
	account.AuthData.RefreshToken = strPtr(credentials.RefreshToken)
	account.AuthData.ExpiresAt = nil
	if credentials.ExpiresAt > 0 {
		expiresAt := credentials.ExpiresAt
		account.AuthData.ExpiresAt = &expiresAt
	}
}

func fetchCodexUsage(client *http.Client, account storedAccount) (providerFetchResult, error) {
	updated, changed, err := ensureFreshTokens(account, client)
	if err != nil {
		return providerFetchResult{}, err
	}
	var usage *rateLimitStatusPayload
	var credits *resetCreditsPayload
	var usageErr, creditsErr error
	done := make(chan struct{}, 2)
	go func() { usage, usageErr = fetchUsage(updated, client); done <- struct{}{} }()
	go func() { credits, creditsErr = fetchResetCredits(updated, client); done <- struct{}{} }()
	<-done
	<-done
	if usageErr != nil {
		return providerFetchResult{}, usageErr
	}
	result := providerFetchResult{
		Account: updated, AccountChanged: changed, RateLimit: usage.RateLimit, ResetError: creditsErr,
		Usage: providerUsage{Plan: usage.PlanType, Metrics: []providerMetric{codexWindowMetric(sessionSlot, "SESSION", usage.RateLimit, true), codexWindowMetric(weeklySlot, "WEEKLY", usage.RateLimit, false)}},
	}
	if creditsErr == nil {
		result.ResetCredits = credits
	}
	return result, nil
}

func codexWindowMetric(slot metricSlot, label string, limits *rateLimitDetails, primary bool) providerMetric {
	window := selectWindow(limits, primary)
	metric := providerMetric{Kind: percentageMetric, Slot: slot, Label: label}
	if window == nil {
		return metric
	}
	used := percentValue(window.UsedPercent)
	metric.Used, metric.ResetAt = &used, window.ResetAt
	return metric
}

func providerCredentialError(err error) bool {
	return providers.IsCredentialError(err)
}

package usage

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/McMelonTV/check-usage/internal/claudeapi"
	"github.com/McMelonTV/check-usage/internal/codexapi"
	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/internal/storage"
)

type ProviderDefinition struct {
	ID           string
	Name         string
	Plan         string
	Credentials  providers.CredentialMode
	ResetCredits bool
}

type providerFetchResult struct {
	Usage          providers.Usage
	Account        storage.Account
	AccountChanged bool
	ResetCredits   *codexapi.ResetCreditsPayload
	ResetError     error
	RateLimit      *codexapi.RateLimitDetails
	// NextFetchAt, when set, is the earliest time to ask the provider again.
	NextFetchAt time.Time
	// PlanCheckedAt, when set, is when the plan was read from the provider.
	PlanCheckedAt time.Time
}

func ProviderDefinitions() []ProviderDefinition {
	definitions := providers.Definitions()
	result := make([]ProviderDefinition, 0, len(definitions))
	for _, definition := range definitions {
		result = append(result, ProviderDefinition{ID: definition.ID, Name: definition.Name, Plan: definition.Plan, Credentials: definition.Credentials, ResetCredits: definition.SupportsResetCredits})
	}
	return result
}

func ProviderFor(id string) (ProviderDefinition, error) {
	definition, ok := providers.Get(id)
	if !ok {
		return ProviderDefinition{}, fmt.Errorf("unsupported provider %q", id)
	}
	return ProviderDefinition{ID: definition.ID, Name: definition.Name, Plan: definition.Plan, Credentials: definition.Credentials, ResetCredits: definition.SupportsResetCredits}, nil
}

func ProviderName(id string) string {
	definition, err := ProviderFor(id)
	if err != nil {
		return id
	}
	return definition.Name
}

func AccountPlan(account storage.Account) string {
	if account.PlanType != nil && *account.PlanType != "" {
		return *account.PlanType
	}
	provider, err := ProviderFor(account.Provider)
	if err != nil {
		return "-"
	}
	return FirstNonEmpty(provider.Plan, "-")
}

func emptyProviderMetrics(providerID string) []providers.Metric {
	switch providerID {
	case providers.Codex, providers.Claude:
		return []providers.Metric{{Kind: providers.Percentage, Slot: providers.SessionSlot, Label: "SESSION"}, {Kind: providers.Percentage, Slot: providers.WeeklySlot, Label: "WEEKLY"}}
	case providers.OpenCodeGo:
		return []providers.Metric{{Kind: providers.Percentage, Slot: providers.SessionSlot, Label: "SESSION"}, {Kind: providers.Percentage, Slot: providers.WeeklySlot, Label: "WEEKLY"}, {Kind: providers.Percentage, Slot: providers.MonthlySlot, Label: "MONTHLY"}}
	case providers.DeepSeek:
		return nil
	case providers.Cursor:
		return providers.CursorMetrics()
	default:
		return nil
	}
}

func FetchProviderUsage(ctx context.Context, client *http.Client, account storage.Account) (providerFetchResult, error) {
	definition, err := ProviderFor(account.Provider)
	if err != nil {
		return providerFetchResult{}, err
	}
	if definition.Credentials == providers.APIKey {
		var usage providers.Usage
		changed := false
		if definition.ID == providers.Cursor {
			var token string
			usage, token, err = providers.FetchCursorUsage(ctx, client, storage.StringValue(account.AuthData.APIKey), storage.StringValue(account.AuthData.AccessToken), time.Now())
			changed = err == nil && token != storage.StringValue(account.AuthData.AccessToken)
			if changed {
				account.AuthData.AccessToken = new(token)
			}
		} else {
			usage, err = providers.FetchAPIKeyUsage(ctx, client, definition.ID, storage.StringValue(account.AuthData.APIKey), UserAgent)
		}
		if err != nil {
			return providerFetchResult{}, err
		}
		if usage.Plan != "" && storage.StringValue(account.PlanType) != usage.Plan {
			account.PlanType = new(usage.Plan)
			changed = true
		}
		return providerFetchResult{Usage: usage, Account: account, AccountChanged: changed}, nil
	}
	if definition.ID == providers.Claude {
		return fetchClaudeUsage(ctx, client, account)
	}
	return fetchCodexUsage(client, account)
}

func fetchClaudeUsage(ctx context.Context, client *http.Client, account storage.Account) (providerFetchResult, error) {
	credentials := claudeapi.Credentials{AccessToken: storage.StringValue(account.AuthData.AccessToken), RefreshToken: storage.StringValue(account.AuthData.RefreshToken)}
	if account.AuthData.ExpiresAt != nil {
		credentials.ExpiresAt = *account.AuthData.ExpiresAt
	}
	now := time.Now()
	var planCheckedAt time.Time
	if entry, ok, err := storage.LoadAccountCache(account.ID); err == nil && ok && entry.PlanCheckedAt > 0 {
		planCheckedAt = time.Unix(entry.PlanCheckedAt, 0)
	}
	fetchPlan := providers.ClaudePlanDue(storage.StringValue(account.PlanType), planCheckedAt, now)
	result, err := providers.FetchClaudeUsage(ctx, client, credentials, claudeapi.DefaultUserAgent, now, fetchPlan)
	changed := result.CredentialsChanged
	if changed {
		storage.SetClaudeCredentials(&account, result.Credentials)
	}
	if err != nil {
		// Keep rotated tokens even when the usage request itself fails.
		fetch := providerFetchResult{Account: account, AccountChanged: changed}
		if delay, limited := claudeapi.RateLimitDelay(err); limited {
			fetch.NextFetchAt = now.Add(delay)
		}
		return fetch, err
	}
	if result.Usage.Plan != "" && storage.StringValue(account.PlanType) != result.Usage.Plan {
		account.PlanType = new(result.Usage.Plan)
		changed = true
	}
	// Keep the account's plan when the profile was not requested this time.
	result.Usage.Plan = FirstNonEmpty(result.Usage.Plan, storage.StringValue(account.PlanType))
	fetch := providerFetchResult{Usage: result.Usage, Account: account, AccountChanged: changed, NextFetchAt: now.Add(claudeapi.MinRefreshInterval)}
	if result.PlanChecked {
		fetch.PlanCheckedAt = now
	}
	return fetch, nil
}

func fetchCodexUsage(client *http.Client, account storage.Account) (providerFetchResult, error) {
	updated, changed, err := EnsureFreshTokens(account, client)
	if err != nil {
		return providerFetchResult{}, err
	}
	var usage *codexapi.RateLimitStatusPayload
	var credits *codexapi.ResetCreditsPayload
	var usageErr, creditsErr error
	done := make(chan struct{}, 2)
	go func() { usage, usageErr = FetchUsage(updated, client); done <- struct{}{} }()
	go func() { credits, creditsErr = FetchResetCredits(updated, client); done <- struct{}{} }()
	<-done
	<-done
	if usageErr != nil {
		return providerFetchResult{}, usageErr
	}
	result := providerFetchResult{
		Account: updated, AccountChanged: changed, RateLimit: usage.RateLimit, ResetError: creditsErr,
		Usage: providers.Usage{Plan: usage.PlanType, Metrics: []providers.Metric{CodexWindowMetric(providers.SessionSlot, "SESSION", usage.RateLimit, true), CodexWindowMetric(providers.WeeklySlot, "WEEKLY", usage.RateLimit, false)}},
	}
	if creditsErr == nil {
		result.ResetCredits = credits
	}
	return result, nil
}

func CodexWindowMetric(slot providers.MetricSlot, label string, limits *codexapi.RateLimitDetails, primary bool) providers.Metric {
	window := selectWindow(limits, primary)
	metric := providers.Metric{Kind: providers.Percentage, Slot: slot, Label: label}
	if window == nil {
		return metric
	}
	used := PercentValue(window.UsedPercent)
	metric.Used, metric.ResetAt = &used, window.ResetAt
	return metric
}

func providerCredentialError(err error) bool {
	return providers.IsCredentialError(err)
}

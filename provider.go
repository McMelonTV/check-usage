package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/McMelonTV/check-usage/providers"
)

const (
	providerCodex      = providers.Codex
	providerOpenCodeGo = providers.OpenCodeGo
	providerDeepSeek   = providers.DeepSeek
	providerCrof       = providers.Crof
	providerCursor     = providers.Cursor
)

type credentialMode = providers.CredentialMode
type providerMetric = providers.Metric
type providerUsage = providers.Usage
type metricKind = providers.MetricKind
type metricSlot = providers.MetricSlot

const (
	deviceCredentials = providers.Device
	apiKeyCredentials = providers.APIKey
	percentageMetric  = providers.Percentage
	textMetric        = providers.Text
	sessionSlot       = providers.SessionSlot
	weeklySlot        = providers.WeeklySlot
	monthlySlot       = providers.MonthlySlot
	cursorModelsSlot  = providers.CursorModelsSlot
	otherModelsSlot   = providers.OtherModelsSlot
	onDemandSlot      = providers.OnDemandSlot
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
	case providerCodex:
		return []providerMetric{{Kind: percentageMetric, Slot: sessionSlot, Label: "SESSION"}, {Kind: percentageMetric, Slot: weeklySlot, Label: "WEEKLY"}}
	case providerOpenCodeGo:
		return []providerMetric{{Kind: percentageMetric, Slot: sessionSlot, Label: "SESSION"}, {Kind: percentageMetric, Slot: weeklySlot, Label: "WEEKLY"}, {Kind: percentageMetric, Slot: monthlySlot, Label: "MONTHLY"}}
	case providerDeepSeek:
		return nil
	case providerCrof:
		return nil
	case providerCursor:
		return []providerMetric{{Kind: percentageMetric, Slot: cursorModelsSlot, Label: "CURSOR MODELS"}, {Kind: percentageMetric, Slot: otherModelsSlot, Label: "OTHER MODELS"}, {Kind: textMetric, Slot: onDemandSlot, Label: "ON-DEMAND"}}
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
	return fetchCodexUsage(client, account)
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

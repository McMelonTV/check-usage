package usageapi

import (
	"context"
	"net/http"

	"github.com/McMelonTV/check-usage/codexapi"
	"github.com/McMelonTV/check-usage/providers"
)

const (
	providerCodex      = providers.Codex
	providerOpenCodeGo = providers.OpenCodeGo
	providerDeepSeek   = providers.DeepSeek
	providerCrof       = providers.Crof
)

type credentialMode = providers.CredentialMode

const (
	deviceCredentials = providers.Device
	apiKeyCredentials = providers.APIKey
)

type providerDefinition struct {
	ID           string
	Name         string
	Plan         string
	Credentials  credentialMode
	ResetCredits bool
}

var providerDefinitions = func() map[string]providerDefinition {
	result := make(map[string]providerDefinition)
	for _, definition := range providers.Definitions() {
		result[definition.ID] = providerDefinition{ID: definition.ID, Name: definition.Name, Plan: definition.Plan, Credentials: definition.Credentials, ResetCredits: definition.SupportsResetCredits}
	}
	return result
}()

func fetchAPIKeyUsage(ctx context.Context, client *http.Client, account storedAccount, userAgent string) (providers.Usage, error) {
	return providers.FetchAPIKeyUsage(ctx, client, account.Provider, stringValue(account.AuthData.APIKey), userAgent)
}

func codexProviderUsage(payload *codexapi.RateLimitStatusPayload) providers.Usage {
	usage := providers.Usage{Plan: payload.PlanType}
	if payload.RateLimit == nil {
		return usage
	}
	usage.Metrics = []providers.Metric{
		codexWindowMetric(providers.SessionSlot, "SESSION", payload.RateLimit, true),
		codexWindowMetric(providers.WeeklySlot, "WEEKLY", payload.RateLimit, false),
	}
	return usage
}

func codexWindowMetric(slot providers.MetricSlot, label string, limits *codexapi.RateLimitDetails, primary bool) providers.Metric {
	window := codexapi.SelectWindow(limits, primary)
	metric := providers.Metric{Kind: providers.Percentage, Slot: slot, Label: label}
	if window == nil {
		return metric
	}
	used := codexapi.PercentValue(window.UsedPercent)
	metric.Used, metric.ResetAt = &used, window.ResetAt
	return metric
}

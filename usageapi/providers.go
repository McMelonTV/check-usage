package usageapi

import (
	"context"
	"net/http"

	"github.com/McMelonTV/codex-usage/codexapi"
	"github.com/McMelonTV/codex-usage/providers"
)

const (
	providerOpenAICodex = providers.OpenAICodex
	providerOpenCodeGo  = providers.OpenCodeGo
	providerDeepSeek    = providers.DeepSeek
)

type credentialMode = providers.CredentialMode

const (
	deviceCredentials = providers.Device
	apiKeyCredentials = providers.APIKey
)

type providerDefinition struct {
	ID           string
	Name         string
	Credentials  credentialMode
	ResetCredits bool
}

var providerDefinitions = func() map[string]providerDefinition {
	result := make(map[string]providerDefinition)
	for _, definition := range providers.Definitions() {
		result[definition.ID] = providerDefinition{ID: definition.ID, Name: definition.Name, Credentials: definition.Credentials, ResetCredits: definition.SupportsResetCredits}
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
	for _, window := range []struct {
		label string
		value *codexapi.RateLimitWindow
	}{{"SESSION", payload.RateLimit.PrimaryWindow}, {"WEEKLY", payload.RateLimit.SecondaryWindow}} {
		if window.value == nil {
			continue
		}
		used := window.value.UsedPercent
		slot := providers.SessionSlot
		if window.label == "WEEKLY" {
			slot = providers.WeeklySlot
		}
		usage.Metrics = append(usage.Metrics, providers.Metric{Kind: providers.Percentage, Slot: slot, Label: window.label, Used: &used, ResetAt: window.value.ResetAt})
	}
	return usage
}

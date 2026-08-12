package usageapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/McMelonTV/codex-usage/codexapi"
)

const (
	providerOpenAICodex = "openai-codex"
	providerOpenCodeGo  = "opencode-go"
	providerDeepSeek    = "deepseek"
)

type credentialMode string

const (
	deviceCredentials credentialMode = "device"
	apiKeyCredentials credentialMode = "api_key"
)

type providerDefinition struct {
	ID           string
	Name         string
	Credentials  credentialMode
	ResetCredits bool
}

var providerDefinitions = map[string]providerDefinition{
	providerOpenAICodex: {ID: providerOpenAICodex, Name: "OpenAI Codex", Credentials: deviceCredentials, ResetCredits: true},
	providerOpenCodeGo:  {ID: providerOpenCodeGo, Name: "OpenCode Go", Credentials: apiKeyCredentials},
	providerDeepSeek:    {ID: providerDeepSeek, Name: "DeepSeek", Credentials: apiKeyCredentials},
}

func normalizeProvider(provider string) string {
	value := strings.ToLower(strings.TrimSpace(provider))
	value = strings.ReplaceAll(value, "_", "-")
	value = strings.ReplaceAll(value, " ", "-")
	switch value {
	case "", "openai", "codex", "chatgpt", "openai-codex":
		return providerOpenAICodex
	case "opencode", "open-code-go", "opencode-go":
		return providerOpenCodeGo
	case "deepseek":
		return providerDeepSeek
	default:
		return value
	}
}

func fetchAPIKeySnapshot(ctx context.Context, client *http.Client, account storedAccount, now time.Time) (*codexapi.UsageSnapshot, error) {
	key := strings.TrimSpace(stringValue(account.AuthData.APIKey))
	if key == "" {
		return nil, fmt.Errorf("account %q is missing an API key", account.Name)
	}
	provider := normalizeProvider(account.Provider)
	endpoint := ""
	switch provider {
	case providerOpenCodeGo:
		endpoint = "https://opencode.ai/zen/go/v1/usage"
	case providerDeepSeek:
		endpoint = "https://api.deepseek.com/user/balance"
	default:
		return nil, fmt.Errorf("provider %q is unsupported", account.Provider)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("%s usage request failed: %s", provider, response.Status)
	}
	snapshot := &codexapi.UsageSnapshot{FetchedAtMillis: now.UnixMilli()}
	if provider == providerOpenCodeGo {
		var payload struct {
			Usage struct {
				Rolling, Weekly, Monthly struct {
					Percent float64 `json:"percent"`
				}
			} `json:"usage"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, fmt.Errorf("decode OpenCode Go usage response: %w", err)
		}
		snapshot.Windows = []codexapi.UsageWindow{apiUsageWindow("ROLLING", payload.Usage.Rolling.Percent), apiUsageWindow("WEEK", payload.Usage.Weekly.Percent), apiUsageWindow("MONTH", payload.Usage.Monthly.Percent)}
		return snapshot, nil
	}
	var payload struct {
		Available bool `json:"is_available"`
		Balances  []struct {
			Currency string `json:"currency"`
			Total    string `json:"total_balance"`
		} `json:"balance_infos"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode DeepSeek balance response: %w", err)
	}
	labels := make([]string, 0, len(payload.Balances))
	for _, balance := range payload.Balances {
		labels = append(labels, strings.TrimSpace(balance.Currency)+" "+strings.TrimSpace(balance.Total))
	}
	if len(labels) == 0 {
		labels = append(labels, "no balance reported")
	}
	snapshot.Windows = []codexapi.UsageWindow{{Kind: "balance", Label: strings.Join(labels, " | ")}}
	if !payload.Available {
		snapshot.CreditsError = "Insufficient balance"
	}
	return snapshot, nil
}

func apiUsageWindow(label string, used float64) codexapi.UsageWindow {
	if used < 0 {
		used = 0
	}
	if used > 100 {
		used = 100
	}
	remaining := 100 - used
	return codexapi.UsageWindow{Kind: strings.ToLower(label), Label: label, UsedPercent: &used, Remaining: &remaining}
}

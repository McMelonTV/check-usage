package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
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
	Fetch        func(context.Context, *http.Client, storedAccount) (providerFetchResult, error)
}

var providerRegistry = map[string]providerDefinition{
	providerOpenAICodex: {ID: providerOpenAICodex, Name: "OpenAI Codex", Credentials: deviceCredentials, ResetCredits: true, Fetch: fetchOpenAICodexUsage},
	providerOpenCodeGo:  {ID: providerOpenCodeGo, Name: "OpenCode Go", Credentials: apiKeyCredentials, Fetch: fetchOpenCodeGoUsage},
	providerDeepSeek:    {ID: providerDeepSeek, Name: "DeepSeek", Credentials: apiKeyCredentials, Fetch: fetchDeepSeekUsage},
}

func providerDefinitions() []providerDefinition {
	return []providerDefinition{providerRegistry[providerOpenAICodex], providerRegistry[providerOpenCodeGo], providerRegistry[providerDeepSeek]}
}

var errMissingAPIKey = errors.New("missing API key")

type providerMetric struct {
	Label   string   `json:"label"`
	Summary string   `json:"summary"`
	Used    *float64 `json:"used_percent,omitempty"`
}

type providerUsage struct {
	Plan      string         `json:"plan,omitempty"`
	Primary   providerMetric `json:"primary"`
	Secondary providerMetric `json:"secondary"`
	Details   providerMetric `json:"details"`
}

type providerFetchResult struct {
	Usage          providerUsage
	Account        storedAccount
	AccountChanged bool
	ResetCredits   *resetCreditsPayload
}

func providerFor(id string) (providerDefinition, error) {
	definition, ok := providerRegistry[normalizeProviderID(id)]
	if !ok {
		return providerDefinition{}, fmt.Errorf("unsupported provider %q", id)
	}
	return definition, nil
}

func providerName(id string) string {
	definition, err := providerFor(id)
	if err != nil {
		return id
	}
	return definition.Name
}

func normalizeProviderID(id string) string {
	value := strings.ToLower(strings.TrimSpace(id))
	value = strings.ReplaceAll(value, "_", "-")
	value = strings.ReplaceAll(value, " ", "-")
	switch value {
	case "openai", "codex", "chatgpt", "openai-codex":
		return providerOpenAICodex
	case "opencode", "open-code-go", "opencode-go":
		return providerOpenCodeGo
	case "deepseek":
		return providerDeepSeek
	default:
		return value
	}
}

func migrateProvider(account storedAccount) (string, bool) {
	if strings.TrimSpace(account.Provider) != "" {
		return normalizeProviderID(account.Provider), normalizeProviderID(account.Provider) != account.Provider
	}
	if normalizeAuthType(account.AuthData.Type) == "chatgpt" {
		return providerOpenAICodex, true
	}
	return "", false
}

func fetchProviderUsage(ctx context.Context, client *http.Client, account storedAccount) (providerFetchResult, error) {
	definition, err := providerFor(account.Provider)
	if err != nil {
		return providerFetchResult{}, err
	}
	return definition.Fetch(ctx, client, account)
}

func fetchOpenAICodexUsage(_ context.Context, client *http.Client, account storedAccount) (providerFetchResult, error) {
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
	now := time.Now()
	result := providerFetchResult{
		Account: updated, AccountChanged: changed,
		Usage: providerUsage{
			Plan:      usage.PlanType,
			Primary:   providerMetric{Label: "5H", Summary: limitSummary(usage.RateLimit, true, now), Used: windowUsedPercent(usage.RateLimit, true)},
			Secondary: providerMetric{Label: "WEEK", Summary: limitSummary(usage.RateLimit, false, now), Used: windowUsedPercent(usage.RateLimit, false)},
			Details:   providerMetric{Label: "RESETS", Summary: "unavailable"},
		},
	}
	if creditsErr == nil {
		result.ResetCredits = credits
		result.Usage.Details.Summary = resetCreditsSummary(credits, now)
	}
	return result, nil
}

func fetchOpenCodeGoUsage(ctx context.Context, client *http.Client, account storedAccount) (providerFetchResult, error) {
	key, err := apiKey(account)
	if err != nil {
		return providerFetchResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://opencode.ai/zen/go/v1/usage", nil)
	if err != nil {
		return providerFetchResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	var body struct {
		Usage struct {
			Rolling providerWindow `json:"rolling"`
			Weekly  providerWindow `json:"weekly"`
			Monthly providerWindow `json:"monthly"`
		} `json:"usage"`
	}
	if err := doProviderJSON(client, req, "OpenCode Go usage request", &body); err != nil {
		return providerFetchResult{}, err
	}
	return providerFetchResult{Account: account, Usage: providerUsage{Primary: body.Usage.Rolling.metric("ROLLING"), Secondary: body.Usage.Weekly.metric("WEEK"), Details: body.Usage.Monthly.metric("MONTH")}}, nil
}

func fetchDeepSeekUsage(ctx context.Context, client *http.Client, account storedAccount) (providerFetchResult, error) {
	key, err := apiKey(account)
	if err != nil {
		return providerFetchResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.deepseek.com/user/balance", nil)
	if err != nil {
		return providerFetchResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	var body struct {
		Available bool `json:"is_available"`
		Balances  []struct {
			Currency string `json:"currency"`
			Total    string `json:"total_balance"`
		} `json:"balance_infos"`
	}
	if err := doProviderJSON(client, req, "DeepSeek balance request", &body); err != nil {
		return providerFetchResult{}, err
	}
	balances := make([]string, 0, len(body.Balances))
	for _, balance := range body.Balances {
		balances = append(balances, strings.TrimSpace(balance.Currency)+" "+strings.TrimSpace(balance.Total))
	}
	availability := "available"
	if !body.Available {
		availability = "insufficient balance"
	}
	return providerFetchResult{Account: account, Usage: providerUsage{Primary: providerMetric{Label: "BALANCE", Summary: firstNonEmpty(strings.Join(balances, " | "), "no balance reported")}, Details: providerMetric{Label: "STATUS", Summary: availability}}}, nil
}

type providerWindow struct {
	Percent  float64 `json:"percent"`
	ResetsAt string  `json:"resetsAt"`
}

func (window providerWindow) metric(label string) providerMetric {
	used := percentValue(window.Percent)
	summary := fmt.Sprintf("%.0f%% used / %.0f%% left", used, 100-used)
	if strings.TrimSpace(window.ResetsAt) != "" {
		summary += " - resets " + window.ResetsAt
	}
	return providerMetric{Label: label, Summary: summary, Used: &used}
}

func apiKey(account storedAccount) (string, error) {
	if account.AuthData.APIKey == nil || strings.TrimSpace(*account.AuthData.APIKey) == "" {
		return "", errMissingAPIKey
	}
	return strings.TrimSpace(*account.AuthData.APIKey), nil
}

func doProviderJSON(client *http.Client, request *http.Request, operation string, output any) error {
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("%s failed: %s: %s", operation, response.Status, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, output); err != nil {
		return fmt.Errorf("decode %s response: %w", operation, err)
	}
	return nil
}

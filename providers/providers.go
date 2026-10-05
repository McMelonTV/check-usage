package providers

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
	Codex      = "codex"
	OpenCodeGo = "opencode-go"
	DeepSeek   = "deepseek"
)

type CredentialMode string

const (
	Device CredentialMode = "device"
	APIKey CredentialMode = "api_key"
)

type Definition struct {
	ID                   string
	Name                 string
	Plan                 string
	Credentials          CredentialMode
	SupportsResetCredits bool
}

var definitions = []Definition{
	{ID: Codex, Name: "Codex", Credentials: Device, SupportsResetCredits: true},
	{ID: OpenCodeGo, Name: "OpenCode", Plan: "Go", Credentials: APIKey},
	{ID: DeepSeek, Name: "DeepSeek", Credentials: APIKey},
}

type MetricKind string

type MetricSlot string

const (
	Percentage MetricKind = "percentage"

	SessionSlot MetricSlot = "session"
	WeeklySlot  MetricSlot = "weekly"
	MonthlySlot MetricSlot = "monthly"
)

type Metric struct {
	Kind    MetricKind `json:"kind"`
	Slot    MetricSlot `json:"slot"`
	Label   string     `json:"label"`
	Used    *float64   `json:"used_percent,omitempty"`
	ResetAt *int64     `json:"reset_at,omitempty"`
}

type Usage struct {
	Plan    string   `json:"plan,omitempty"`
	Metrics []Metric `json:"metrics"`
}

type HTTPError struct {
	Operation  string
	StatusCode int
	Status     string
	Body       string
}

func (err *HTTPError) Error() string {
	return fmt.Sprintf("%s failed: %s: %s", err.Operation, err.Status, err.Body)
}

var ErrMissingAPIKey = errors.New("missing API key")

func IsCredentialError(err error) bool {
	if errors.Is(err, ErrMissingAPIKey) {
		return true
	}
	var httpErr *HTTPError
	return errors.As(err, &httpErr) && (httpErr.StatusCode == http.StatusUnauthorized || httpErr.StatusCode == http.StatusForbidden)
}

func Definitions() []Definition {
	return append([]Definition(nil), definitions...)
}

func Get(id string) (Definition, bool) {
	for _, definition := range definitions {
		if definition.ID == id {
			return definition, true
		}
	}
	return Definition{}, false
}
func FetchAPIKeyUsage(ctx context.Context, client *http.Client, providerID, key, userAgent string) (Usage, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return Usage{}, ErrMissingAPIKey
	}
	switch providerID {
	case OpenCodeGo:
		return fetchOpenCodeGo(ctx, client, key, userAgent)
	case DeepSeek:
		return fetchDeepSeek(ctx, client, key, userAgent)
	default:
		return Usage{}, fmt.Errorf("provider %q does not support API-key usage", providerID)
	}
}

type usageWindow struct {
	Percent  *float64 `json:"percent"`
	ResetsAt *string  `json:"resetsAt"`
}

func fetchOpenCodeGo(ctx context.Context, client *http.Client, key, userAgent string) (Usage, error) {
	req, err := providerRequest(ctx, "https://opencode.ai/zen/go/v1/usage", key, userAgent)
	if err != nil {
		return Usage{}, err
	}
	var payload struct {
		Usage struct {
			Session *usageWindow `json:"rolling"`
			Weekly  *usageWindow `json:"weekly"`
			Monthly *usageWindow `json:"monthly"`
		} `json:"usage"`
	}
	if err := doJSON(client, req, "OpenCode usage request", &payload); err != nil {
		return Usage{}, err
	}
	session, err := payload.Usage.Session.metric(SessionSlot, "SESSION")
	if err != nil {
		return Usage{}, err
	}
	weekly, err := payload.Usage.Weekly.metric(WeeklySlot, "WEEKLY")
	if err != nil {
		return Usage{}, err
	}
	monthly, err := payload.Usage.Monthly.metric(MonthlySlot, "MONTHLY")
	if err != nil {
		return Usage{}, err
	}
	return Usage{Plan: "Go", Metrics: []Metric{session, weekly, monthly}}, nil
}

func (window *usageWindow) metric(slot MetricSlot, label string) (Metric, error) {
	if window == nil || window.Percent == nil || window.ResetsAt == nil {
		return Metric{}, fmt.Errorf("OpenCode %s usage is missing required fields", strings.ToLower(label))
	}
	reset, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(*window.ResetsAt))
	if err != nil {
		return Metric{}, fmt.Errorf("parse OpenCode %s reset time: %w", strings.ToLower(label), err)
	}
	used, resetAt := clampPercent(*window.Percent), reset.Unix()
	return Metric{Kind: Percentage, Slot: slot, Label: label, Used: &used, ResetAt: &resetAt}, nil
}

func fetchDeepSeek(ctx context.Context, client *http.Client, key, userAgent string) (Usage, error) {
	req, err := providerRequest(ctx, "https://api.deepseek.com/user/balance", key, userAgent)
	if err != nil {
		return Usage{}, err
	}
	var payload struct {
		Available *bool `json:"is_available"`
		Balances  []struct {
			Currency string `json:"currency"`
			Total    string `json:"total_balance"`
		} `json:"balance_infos"`
	}
	if err := doJSON(client, req, "DeepSeek balance request", &payload); err != nil {
		return Usage{}, err
	}
	if payload.Available == nil || payload.Balances == nil {
		return Usage{}, fmt.Errorf("DeepSeek balance response is missing required fields")
	}
	balances := make([]string, 0, len(payload.Balances))
	for _, balance := range payload.Balances {
		currency, total := strings.TrimSpace(balance.Currency), strings.TrimSpace(balance.Total)
		if currency == "" || total == "" {
			continue
		}
		balances = append(balances, currency+" "+total)
	}
	plan := strings.Join(balances, " | ")
	if plan == "" {
		plan = "no balance reported"
	}
	return Usage{Plan: plan}, nil
}

func providerRequest(ctx context.Context, endpoint, key, userAgent string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	if strings.TrimSpace(userAgent) != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	return req, nil
}

func doJSON(client *http.Client, request *http.Request, operation string, output any) error {
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := strings.TrimSpace(string(body))
		if len(message) > 300 {
			message = message[:300] + "..."
		}
		return &HTTPError{Operation: operation, StatusCode: response.StatusCode, Status: response.Status, Body: message}
	}
	if err := json.Unmarshal(body, output); err != nil {
		return fmt.Errorf("decode %s response: %w", operation, err)
	}
	return nil
}

func clampPercent(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

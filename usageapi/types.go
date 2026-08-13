// Package usageapi exposes the application capabilities of codex-usage without
// requiring callers to parse terminal output or manage credential files.
package usageapi

import (
	"github.com/McMelonTV/codex-usage/codexapi"
	"github.com/McMelonTV/codex-usage/providers"
)

// ProtocolVersion is the compatibility version returned by rpc.discover.
const ProtocolVersion = "1.0"

// Account is the public, credential-free representation of a saved account.
type Account struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Email    string `json:"email,omitempty"`
	PlanType string `json:"plan_type,omitempty"`
	AuthType string `json:"auth_type"`
}

// Settings contains the preferences shared with the terminal dashboard.
type Settings struct {
	UsageDisplay       string `json:"usage_display"`
	BarFill            string `json:"bar_fill"`
	PercentagePosition string `json:"percentage_position"`
	ColorTheme         string `json:"color_theme"`
	AutoRefreshSeconds int    `json:"auto_refresh_seconds"`
	CompactMode        bool   `json:"compact_mode"`
}

// DeviceAuthSession contains the values an app displays after beginning login.
type DeviceAuthSession struct {
	Provider            string `json:"provider"`
	SessionID           string `json:"session_id"`
	UserCode            string `json:"user_code"`
	VerificationURL     string `json:"verification_url"`
	PollIntervalSeconds int    `json:"poll_interval_seconds"`
}

// DeviceAuthPoll identifies a device flow that should be polled once.
type DeviceAuthPoll struct {
	Provider  string `json:"provider"`
	SessionID string `json:"session_id"`
	UserCode  string `json:"user_code"`
	Name      string `json:"name,omitempty"`
}

// DeviceAuthResult is pending or complete. Complete results include the saved account.
type DeviceAuthResult struct {
	Status  string   `json:"status"`
	Action  string   `json:"action,omitempty"`
	Account *Account `json:"account,omitempty"`
}

// UsageResult contains one account snapshot or an account-scoped provider error.
type UsageResult struct {
	Account  Account                 `json:"account"`
	Snapshot *codexapi.UsageSnapshot `json:"snapshot,omitempty"`
	Metrics  []providers.Metric      `json:"metrics,omitempty"`
	Cached   bool                    `json:"cached"`
	Error    string                  `json:"error,omitempty"`
}

// ResetCreditsResult contains reset-credit details for one account.
type ResetCreditsResult struct {
	Account Account                       `json:"account"`
	Credits *codexapi.ResetCreditsPayload `json:"credits,omitempty"`
	Cached  bool                          `json:"cached"`
}

// AccountMutation describes a persisted account change.
type AccountMutation struct {
	Action  string  `json:"action"`
	Account Account `json:"account"`
}

// APIKeyAccount creates or updates an account for a provider authenticated by API key.
// The key is accepted only for this call and is never returned in results.
type APIKeyAccount struct {
	Account  string `json:"account,omitempty"`
	Provider string `json:"provider"`
	Name     string `json:"name,omitempty"`
	APIKey   string `json:"api_key"`
}

type storedAccount struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Provider string   `json:"provider"`
	Email    *string  `json:"email,omitempty"`
	PlanType *string  `json:"plan_type,omitempty"`
	AuthData authData `json:"auth_data"`
}

type authData struct {
	Type         string  `json:"type"`
	APIKey       *string `json:"api_key,omitempty"`
	IDToken      *string `json:"id_token,omitempty"`
	AccessToken  *string `json:"access_token,omitempty"`
	RefreshToken *string `json:"refresh_token,omitempty"`
	AccountID    *string `json:"account_id,omitempty"`
}

type accountsStore struct {
	Accounts []storedAccount `json:"accounts"`
}

type cacheEntry struct {
	PlanType       string                        `json:"plan_type,omitempty"`
	RateLimit      *codexapi.RateLimitDetails    `json:"rate_limit,omitempty"`
	ResetCredits   *codexapi.ResetCreditsPayload `json:"reset_credits,omitempty"`
	FetchedAt      int64                         `json:"fetched_at"`
	ResetFetchedAt int64                         `json:"reset_fetched_at,omitempty"`
	ProviderUsage  *providers.Usage              `json:"provider_usage,omitempty"`
}

func (account storedAccount) public() Account {
	plan := stringValue(account.PlanType)
	if plan == "" {
		if provider, ok := providerDefinitions[account.Provider]; ok {
			plan = provider.Plan
		}
	}
	return Account{
		ID: account.ID, Name: account.Name, Provider: account.Provider,
		Email: stringValue(account.Email), PlanType: plan,
		AuthType: account.AuthData.Type,
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

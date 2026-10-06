package storage

import (
	"strings"

	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/internal/providers/claudeapi"
	"github.com/McMelonTV/check-usage/internal/providers/codexapi"
)

func StringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func OptionalString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return new(value)
}

func SetClaudeCredentials(account *Account, credentials claudeapi.Credentials) {
	account.AuthData.AccessToken = new(credentials.AccessToken)
	account.AuthData.RefreshToken = new(credentials.RefreshToken)
	account.AuthData.ExpiresAt = nil
	if credentials.ExpiresAt > 0 {
		expiresAt := credentials.ExpiresAt
		account.AuthData.ExpiresAt = &expiresAt
	}
}

func (s Settings) BarVisible() bool { return s.ShowBar == nil || *s.ShowBar }

func (s Settings) PercentVisible() bool { return s.ShowPercent == nil || *s.ShowPercent }

func (s Settings) ResetVisible() bool { return s.ShowReset == nil || *s.ShowReset }

type AccountStore struct {
	Accounts  []Account `json:"accounts"`
	NeedsSave bool      `json:"-"`
}

type Account struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Provider string   `json:"provider"`
	Email    *string  `json:"email,omitempty"`
	PlanType *string  `json:"plan_type,omitempty"`
	AuthData AuthData `json:"auth_data"`
}

type AuthData struct {
	Type         string  `json:"type"`
	APIKey       *string `json:"api_key,omitempty"`
	IDToken      *string `json:"id_token,omitempty"`
	AccessToken  *string `json:"access_token,omitempty"`
	RefreshToken *string `json:"refresh_token,omitempty"`
	AccountID    *string `json:"account_id,omitempty"`
	ExpiresAt    *int64  `json:"expires_at,omitempty"`
}

type Settings struct {
	UsageDisplay       string `json:"usage_display"`
	BarFill            string `json:"bar_fill"`
	BarOrder           string `json:"bar_order"`
	ShowBar            *bool  `json:"show_bar,omitempty"`
	ShowPercent        *bool  `json:"show_percent,omitempty"`
	ShowReset          *bool  `json:"show_reset,omitempty"`
	ColorTheme         string `json:"color_theme"`
	AutoRefreshSeconds int    `json:"auto_refresh_seconds"`
	CompactMode        bool   `json:"compact_mode"`
}

type CacheEntry struct {
	PlanType       string                        `json:"plan_type,omitempty"`
	RateLimit      *codexapi.RateLimitDetails    `json:"rate_limit,omitempty"`
	ResetCredits   *codexapi.ResetCreditsPayload `json:"reset_credits,omitempty"`
	FetchedAt      int64                         `json:"fetched_at"`
	ResetFetchedAt int64                         `json:"reset_fetched_at,omitempty"`
	ProviderUsage  *providers.Usage              `json:"provider_usage,omitempty"`
	// NextFetchAt (unix seconds) throttles rate-limited providers: until then
	// the cached usage is shown instead of asking the provider again.
	NextFetchAt int64 `json:"next_fetch_at,omitempty"`
	// PlanCheckedAt (unix seconds) is when the plan was last read from the provider.
	PlanCheckedAt int64 `json:"plan_checked_at,omitempty"`
}

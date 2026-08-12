package main

import "github.com/McMelonTV/codex-usage/codexapi"

type accountsStore struct {
	Accounts  []storedAccount `json:"accounts"`
	needsSave bool
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
	IDToken      *string `json:"id_token,omitempty"`
	AccessToken  *string `json:"access_token,omitempty"`
	RefreshToken *string `json:"refresh_token,omitempty"`
	AccountID    *string `json:"account_id,omitempty"`
}

type rateLimitStatusPayload = codexapi.RateLimitStatusPayload
type rateLimitDetails = codexapi.RateLimitDetails
type rateLimitWindow = codexapi.RateLimitWindow
type creditStatus = codexapi.CreditStatus
type resetCreditsPayload = codexapi.ResetCreditsPayload
type resetCreditDetail = codexapi.ResetCreditDetail

type usageRow struct {
	ID            string
	Name          string
	Provider      string
	Email         string
	Plan          string
	Primary       string
	Secondary     string
	ResetCredits  string
	SortName      string
	PrimaryUsed   *float64
	SecondaryUsed *float64
	Loading       bool
	AuthRequired  bool
	Stale         bool
	ResetsStale   bool
}

type appSettings struct {
	UsageDisplay       string `json:"usage_display"`
	BarFill            string `json:"bar_fill"`
	PercentagePosition string `json:"percentage_position"`
	ColorTheme         string `json:"color_theme"`
	AutoRefreshSeconds int    `json:"auto_refresh_seconds"`
	CompactMode        bool   `json:"compact_mode"`
}

type usageCacheEntry struct {
	PlanType       string               `json:"plan_type,omitempty"`
	RateLimit      *rateLimitDetails    `json:"rate_limit,omitempty"`
	ResetCredits   *resetCreditsPayload `json:"reset_credits,omitempty"`
	FetchedAt      int64                `json:"fetched_at"`
	ResetFetchedAt int64                `json:"reset_fetched_at,omitempty"`
}

type accountResult struct {
	Index          int
	Row            usageRow
	Updated        storedAccount
	TokenRefreshed bool
	Cache          *usageCacheEntry
}

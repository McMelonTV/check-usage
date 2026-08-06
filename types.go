package main

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

type rateLimitStatusPayload struct {
	PlanType  string            `json:"plan_type"`
	RateLimit *rateLimitDetails `json:"rate_limit"`
	Credits   *creditStatus     `json:"credits"`
}

type rateLimitDetails struct {
	PrimaryWindow   *rateLimitWindow `json:"primary_window"`
	SecondaryWindow *rateLimitWindow `json:"secondary_window"`
}

type rateLimitWindow struct {
	UsedPercent        float64 `json:"used_percent"`
	LimitWindowSeconds *int    `json:"limit_window_seconds"`
	ResetAt            *int64  `json:"reset_at"`
}

type creditStatus struct {
	HasCredits bool    `json:"has_credits"`
	Unlimited  bool    `json:"unlimited"`
	Balance    *string `json:"balance"`
}

type resetCreditsPayload struct {
	AvailableCount   int                 `json:"available_count"`
	TotalEarnedCount int                 `json:"total_earned_count"`
	Credits          []resetCreditDetail `json:"credits"`
}

type resetCreditDetail struct {
	Status          string `json:"status"`
	Title           string `json:"title"`
	GrantedAt       string `json:"granted_at"`
	ExpiresAt       string `json:"expires_at"`
	RedeemStartedAt string `json:"redeem_started_at"`
	RedeemedAt      string `json:"redeemed_at"`
}

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

package main

type accountsStore struct {
	Accounts []storedAccount `json:"accounts"`
}

type storedAccount struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
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
	Name          string
	Email         string
	Plan          string
	Primary       string
	Secondary     string
	ResetCredits  string
	SortName      string
	PrimaryUsed   *float64
	SecondaryUsed *float64
}

type accountResult struct {
	Index          int
	Row            usageRow
	Updated        storedAccount
	TokenRefreshed bool
}

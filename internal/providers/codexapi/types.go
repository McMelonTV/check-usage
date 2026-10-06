package codexapi

// Credentials are the OAuth values required to call the Codex usage APIs.
type Credentials struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	AccountID    string `json:"account_id,omitempty"`
}

type TokenResponse struct {
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type DeviceUserCodeResponse struct {
	DeviceAuthID string `json:"device_auth_id"`
	UserCode     string `json:"user_code"`
	Interval     string `json:"interval"`
}

type DeviceTokenPollResponse struct {
	AuthorizationCode string `json:"authorization_code"`
	CodeChallenge     string `json:"code_challenge"`
	CodeVerifier      string `json:"code_verifier"`
}

type RateLimitStatusPayload struct {
	PlanType  string            `json:"plan_type"`
	RateLimit *RateLimitDetails `json:"rate_limit"`
	Credits   *CreditStatus     `json:"credits"`
}

type RateLimitDetails struct {
	PrimaryWindow   *RateLimitWindow `json:"primary_window"`
	SecondaryWindow *RateLimitWindow `json:"secondary_window"`
}

type RateLimitWindow struct {
	UsedPercent        float64 `json:"used_percent"`
	LimitWindowSeconds *int    `json:"limit_window_seconds"`
	ResetAt            *int64  `json:"reset_at"`
}

type CreditStatus struct {
	HasCredits bool    `json:"has_credits"`
	Unlimited  bool    `json:"unlimited"`
	Balance    *string `json:"balance"`
}

type ResetCreditsPayload struct {
	AvailableCount   int                 `json:"available_count"`
	TotalEarnedCount int                 `json:"total_earned_count"`
	Credits          []ResetCreditDetail `json:"credits"`
}

type ResetCreditDetail struct {
	Status          string `json:"status"`
	Title           string `json:"title"`
	GrantedAt       string `json:"granted_at"`
	ExpiresAt       string `json:"expires_at"`
	RedeemStartedAt string `json:"redeem_started_at"`
	RedeemedAt      string `json:"redeemed_at"`
}

type Identity struct {
	Email     string `json:"email,omitempty"`
	PlanType  string `json:"plan_type,omitempty"`
	AccountID string `json:"account_id,omitempty"`
}

type UsageWindow struct {
	Kind          string   `json:"kind"`
	Label         string   `json:"label"`
	UsedPercent   *float64 `json:"used_percent,omitempty"`
	Remaining     *float64 `json:"remaining_percent,omitempty"`
	ResetAt       *int64   `json:"reset_at,omitempty"`
	WindowSeconds *int     `json:"window_seconds,omitempty"`
}

type CreditMetric struct {
	AvailableCount      int    `json:"available_count"`
	TotalEarnedCount    int    `json:"total_earned_count"`
	EarliestExpiryEpoch *int64 `json:"earliest_expiry_epoch_seconds,omitempty"`
}

type UsageSnapshot struct {
	PlanType        string        `json:"plan_type,omitempty"`
	Windows         []UsageWindow `json:"windows"`
	Credits         *CreditMetric `json:"credits,omitempty"`
	FetchedAtMillis int64         `json:"fetched_at_epoch_millis"`
	CreditsError    string        `json:"credits_error,omitempty"`
}

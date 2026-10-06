// Package claudeapi talks to the Claude subscription OAuth and usage endpoints
// used by Claude Code.
package claudeapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	ClientID          = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	AuthorizeURL      = "https://claude.com/cai/oauth/authorize"
	TokenURL          = "https://platform.claude.com/v1/oauth/token"
	ManualRedirectURI = "https://platform.claude.com/oauth/code/callback"
	Scopes            = "user:profile user:inference"
	UsageURL          = "https://api.anthropic.com/api/oauth/usage"
	ProfileURL        = "https://api.anthropic.com/api/oauth/profile"
	OAuthBeta         = "oauth-2025-04-20"
	DefaultUserAgent  = "check-usage/1.0.0"
	refreshSkew       = 60 * time.Second
	// defaultTokenLifetime is assumed when a token response omits expires_in.
	defaultTokenLifetime = 3600
)

type HTTPError struct {
	Operation  string
	StatusCode int
	Status     string
	Body       string
	// RetryAfter is the server's Retry-After delay, when it sent one.
	RetryAfter time.Duration
}

// The usage endpoint is tightly rate limited. MinRefreshInterval matches how
// often Claude Code and similar tools poll it, and RateLimitBackoff is how
// long to wait after a 429 that carries no Retry-After header.
const (
	MinRefreshInterval = 5 * time.Minute
	RateLimitBackoff   = 5 * time.Minute
)

// RateLimitDelay reports whether err is a 429 and how long to wait before retrying.
func RateLimitDelay(err error) (time.Duration, bool) {
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusTooManyRequests {
		return 0, false
	}
	return max(httpErr.RetryAfter, RateLimitBackoff), true
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("%s failed: %s: %s", e.Operation, e.Status, e.Body)
}

var ErrMissingCredentials = errors.New("missing Claude access/refresh token")

// ErrUsageUnavailable means the account is signed in but its organization does
// not allow OAuth usage access, or it has no active subscription.
var ErrUsageUnavailable = errors.New("Claude usage is unavailable for this organization (no active subscription or OAuth disabled)")

// IsAuthenticationError reports whether the user must sign in again.
func IsAuthenticationError(err error) bool {
	if errors.Is(err, ErrMissingCredentials) {
		return true
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		return false
	}
	return httpErr.StatusCode == http.StatusUnauthorized || httpErr.StatusCode == http.StatusForbidden ||
		(httpErr.Operation == "Claude token refresh" && httpErr.StatusCode >= 400 && httpErr.StatusCode < 500)
}

// Credentials are the persisted OAuth tokens. ExpiresAt is unix seconds.
type Credentials struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    int64
}

// AuthSession holds the PKCE values needed to finish a login.
type AuthSession struct {
	State    string
	Verifier string
	URL      string
}

// Login is the result of a completed authorization.
type Login struct {
	Credentials Credentials
	Email       string
	AccountUUID string
	Plan        string
}

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Account      *struct {
		UUID         string `json:"uuid"`
		EmailAddress string `json:"email_address"`
	} `json:"account,omitempty"`
}

type Profile struct {
	Account struct {
		UUID  string `json:"uuid"`
		Email string `json:"email"`
	} `json:"account"`
	Organization struct {
		OrganizationType string `json:"organization_type"`
		RateLimitTier    string `json:"rate_limit_tier"`
	} `json:"organization"`
}

type UsageWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    *string  `json:"resets_at"`
}

// UsageLimit is one entry of the structured limits array, e.g.
// {"kind":"session","percent":58,"resets_at":"..."}.
type UsageLimit struct {
	Kind     string   `json:"kind"`
	Percent  *float64 `json:"percent"`
	ResetsAt *string  `json:"resets_at"`
	Scope    *struct {
		Model *struct {
			DisplayName string `json:"display_name"`
		} `json:"model"`
	} `json:"scope,omitempty"`
}

// ModelName returns the model a weekly_scoped limit applies to.
func (limit UsageLimit) ModelName() string {
	if limit.Scope == nil || limit.Scope.Model == nil {
		return ""
	}
	return strings.TrimSpace(limit.Scope.Model.DisplayName)
}

// preferredScopedModel is shown when several per-model weekly limits exist.
const preferredScopedModel = "Fable"

type UsagePayload struct {
	FiveHour *UsageWindow `json:"five_hour"`
	SevenDay *UsageWindow `json:"seven_day"`
	Limits   []UsageLimit `json:"limits"`
}

// Session returns the 5-hour window, preferring the structured limits array.
func (payload *UsagePayload) Session() *UsageWindow {
	return payload.window("session", payload.FiveHour)
}

// Weekly returns the all-models 7-day window, preferring the structured limits array.
func (payload *UsagePayload) Weekly() *UsageWindow {
	return payload.window("weekly_all", payload.SevenDay)
}

// ModelWeekly returns one per-model weekly window and its model name,
// preferring Fable, otherwise the first scoped limit the API lists.
func (payload *UsagePayload) ModelWeekly() (string, *UsageWindow) {
	var chosen *UsageLimit
	for index := range payload.Limits {
		limit := &payload.Limits[index]
		if limit.Kind != "weekly_scoped" || limit.Percent == nil || limit.ModelName() == "" {
			continue
		}
		if chosen == nil || strings.EqualFold(limit.ModelName(), preferredScopedModel) {
			chosen = limit
		}
		if strings.EqualFold(limit.ModelName(), preferredScopedModel) {
			break
		}
	}
	if chosen == nil {
		return "", nil
	}
	return chosen.ModelName(), &UsageWindow{Utilization: chosen.Percent, ResetsAt: chosen.ResetsAt}
}

func (payload *UsagePayload) window(kind string, fallback *UsageWindow) *UsageWindow {
	for _, limit := range payload.Limits {
		if limit.Kind == kind && limit.Percent != nil {
			return &UsageWindow{Utilization: limit.Percent, ResetsAt: limit.ResetsAt}
		}
	}
	return fallback
}

// NewAuthSession creates PKCE values and the URL the user opens to sign in.
// After approving, claude.com shows a "code#state" string to paste back.
func NewAuthSession() (AuthSession, error) {
	verifier, err := randomString(32)
	if err != nil {
		return AuthSession{}, err
	}
	state, err := randomString(32)
	if err != nil {
		return AuthSession{}, err
	}
	digest := sha256.Sum256([]byte(verifier))
	values := url.Values{}
	values.Set("code", "true")
	values.Set("client_id", ClientID)
	values.Set("response_type", "code")
	values.Set("redirect_uri", ManualRedirectURI)
	values.Set("scope", Scopes)
	values.Set("code_challenge", base64.RawURLEncoding.EncodeToString(digest[:]))
	values.Set("code_challenge_method", "S256")
	values.Set("state", state)
	return AuthSession{State: state, Verifier: verifier, URL: AuthorizeURL + "?" + values.Encode()}, nil
}

// SessionID encodes the session for stateless callers such as the RPC API.
func (session AuthSession) SessionID() string {
	return session.State + "." + session.Verifier
}

// ParseSessionID reverses SessionID.
func ParseSessionID(id string) (AuthSession, error) {
	state, verifier, ok := strings.Cut(strings.TrimSpace(id), ".")
	if !ok || state == "" || verifier == "" {
		return AuthSession{}, fmt.Errorf("invalid Claude session_id")
	}
	return AuthSession{State: state, Verifier: verifier}, nil
}

// CompleteLogin exchanges the pasted "code#state" value for tokens and loads
// the account profile. The profile is best-effort and only supplies the plan.
func CompleteLogin(ctx context.Context, client *http.Client, session AuthSession, pasted, userAgent string, now time.Time) (Login, error) {
	code, state, hasState := strings.Cut(strings.TrimSpace(pasted), "#")
	code = strings.TrimSpace(code)
	if code == "" {
		return Login{}, fmt.Errorf("authorization code cannot be empty")
	}
	if hasState && strings.TrimSpace(state) != session.State {
		return Login{}, fmt.Errorf("authorization code does not belong to this login; start again")
	}
	tokens, err := requestTokens(ctx, client, map[string]string{
		"grant_type": "authorization_code", "code": code, "state": session.State,
		"redirect_uri": ManualRedirectURI, "client_id": ClientID, "code_verifier": session.Verifier,
	}, "Claude token exchange")
	if err != nil {
		return Login{}, err
	}
	if strings.TrimSpace(tokens.RefreshToken) == "" {
		return Login{}, fmt.Errorf("Claude token exchange response missing refresh token")
	}
	login := Login{Credentials: credentialsFrom(tokens, "", now)}
	if tokens.Account != nil {
		login.Email, login.AccountUUID = tokens.Account.EmailAddress, tokens.Account.UUID
	}
	if profile, err := FetchProfile(ctx, client, tokens.AccessToken, userAgent); err == nil {
		login.Plan = profile.PlanName()
		if login.Email == "" {
			login.Email = profile.Account.Email
		}
		if login.AccountUUID == "" {
			login.AccountUUID = profile.Account.UUID
		}
	}
	return login, nil
}

// RefreshCredentials refreshes tokens that are expired, near expiry, or have an
// unknown expiry. The second result reports whether the credentials changed.
func RefreshCredentials(ctx context.Context, client *http.Client, current Credentials, now time.Time) (Credentials, bool, error) {
	if strings.TrimSpace(current.AccessToken) == "" || strings.TrimSpace(current.RefreshToken) == "" {
		return current, false, ErrMissingCredentials
	}
	if current.ExpiresAt > 0 && now.Add(refreshSkew).Unix() < current.ExpiresAt {
		return current, false, nil
	}
	tokens, err := requestTokens(ctx, client, map[string]string{
		"grant_type": "refresh_token", "refresh_token": current.RefreshToken, "client_id": ClientID, "scope": Scopes,
	}, "Claude token refresh")
	if err != nil {
		return current, false, err
	}
	return credentialsFrom(tokens, current.RefreshToken, now), true, nil
}

func FetchUsage(ctx context.Context, client *http.Client, accessToken, userAgent string) (*UsagePayload, error) {
	var out UsagePayload
	if err := getAuthenticated(ctx, client, UsageURL, accessToken, userAgent, "Claude usage request", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func FetchProfile(ctx context.Context, client *http.Client, accessToken, userAgent string) (*Profile, error) {
	var out Profile
	if err := getAuthenticated(ctx, client, ProfileURL, accessToken, userAgent, "Claude profile request", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PlanName returns a short display name such as "Pro" or "Max 20x".
func (profile Profile) PlanName() string {
	tier := strings.ToLower(profile.Organization.RateLimitTier)
	switch {
	case strings.Contains(tier, "max_20x"):
		return "Max 20x"
	case strings.Contains(tier, "max_5x"):
		return "Max 5x"
	}
	switch profile.Organization.OrganizationType {
	case "claude_max":
		return "Max"
	case "claude_pro":
		return "Pro"
	case "claude_team":
		return "Team"
	case "claude_enterprise":
		return "Enterprise"
	}
	return ""
}

// ResetAt parses the window reset time as unix seconds.
func (window *UsageWindow) ResetAt() (int64, bool) {
	if window == nil || window.ResetsAt == nil {
		return 0, false
	}
	reset, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(*window.ResetsAt))
	if err != nil {
		return 0, false
	}
	return reset.Unix(), true
}

func credentialsFrom(tokens *TokenResponse, previousRefreshToken string, now time.Time) Credentials {
	lifetime := tokens.ExpiresIn
	if lifetime <= 0 {
		lifetime = defaultTokenLifetime
	}
	return Credentials{
		AccessToken:  tokens.AccessToken,
		RefreshToken: firstNonEmpty(tokens.RefreshToken, previousRefreshToken),
		ExpiresAt:    now.Unix() + lifetime,
	}
}

func requestTokens(ctx context.Context, client *http.Client, body map[string]string, operation string) (*TokenResponse, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, TokenURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	var out TokenResponse
	if err := doJSON(client, req, operation, &out); err != nil {
		return nil, err
	}
	if strings.TrimSpace(out.AccessToken) == "" {
		return nil, fmt.Errorf("%s response missing access token", operation)
	}
	return &out, nil
}

func getAuthenticated(ctx context.Context, client *http.Client, endpoint, accessToken, userAgent, operation string, out any) error {
	if strings.TrimSpace(accessToken) == "" {
		return ErrMissingCredentials
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("anthropic-beta", OAuthBeta)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", firstNonEmpty(strings.TrimSpace(userAgent), DefaultUserAgent))
	return doJSON(client, req, operation, out)
}

func doJSON(client *http.Client, req *http.Request, operation string, out any) error {
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusForbidden && strings.Contains(string(body), "oauth_not_allowed_for_organization") {
		return ErrUsageUnavailable
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		text := strings.TrimSpace(string(body))
		if len(text) > 300 {
			text = text[:300] + "..."
		}
		httpErr := &HTTPError{Operation: operation, StatusCode: resp.StatusCode, Status: resp.Status, Body: text}
		if seconds, err := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After"))); err == nil && seconds > 0 {
			httpErr.RetryAfter = time.Duration(seconds) * time.Second
		}
		return httpErr
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode %s response: %w", operation, err)
	}
	return nil
}

func randomString(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

package codexapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

func RefreshCredentials(ctx context.Context, client *http.Client, current Credentials, now time.Time) (Credentials, bool, error) {
	if strings.TrimSpace(current.AccessToken) == "" || strings.TrimSpace(current.RefreshToken) == "" {
		return current, false, fmt.Errorf("missing access/refresh token")
	}
	if !TokenExpiredOrNear(current.AccessToken, now, 60*time.Second) {
		return current, false, nil
	}
	refreshed, err := RefreshTokens(ctx, client, current.RefreshToken)
	if err != nil {
		return current, false, err
	}
	current.AccessToken = refreshed.AccessToken
	if refreshed.RefreshToken != "" {
		current.RefreshToken = refreshed.RefreshToken
	}
	if refreshed.IDToken != "" {
		current.IDToken = refreshed.IDToken
	}
	return current, true, nil
}

func ParseIdentity(idToken string) (Identity, error) {
	claims, err := JWTClaims(idToken)
	if err != nil {
		return Identity{}, err
	}
	identity := Identity{Email: stringClaim(claims, "email")}
	if auth, ok := claims["https://api.openai.com/auth"].(map[string]any); ok {
		identity.PlanType = stringClaim(auth, "chatgpt_plan_type")
		identity.AccountID = stringClaim(auth, "chatgpt_account_id")
	}
	if identity.PlanType == "" {
		identity.PlanType = stringClaim(claims, "chatgpt_plan_type")
	}
	if identity.AccountID == "" {
		identity.AccountID = stringClaim(claims, "chatgpt_account_id")
	}
	return identity, nil
}

func JWTClaims(token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode JWT: %w", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("decode JWT claims: %w", err)
	}
	return claims, nil
}

func JWTExpiry(token string) (int64, bool) {
	claims, err := JWTClaims(token)
	if err != nil {
		return 0, false
	}
	switch value := claims["exp"].(type) {
	case float64:
		return int64(value), value != 0
	case json.Number:
		n, err := value.Int64()
		return n, err == nil && n != 0
	case string:
		n, err := strconv.ParseInt(value, 10, 64)
		return n, err == nil && n != 0
	default:
		return 0, false
	}
}

func TokenExpiredOrNear(token string, now time.Time, skew time.Duration) bool {
	expiresAt, ok := JWTExpiry(token)
	return !ok || expiresAt <= now.Add(skew).Unix()
}

func FetchSnapshot(ctx context.Context, client *http.Client, accessToken, accountID, userAgent string, now time.Time) (*UsageSnapshot, error) {
	var usage *RateLimitStatusPayload
	var credits *ResetCreditsPayload
	var usageErr, creditsErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		usage, usageErr = FetchUsage(ctx, client, accessToken, accountID, userAgent)
	}()
	go func() {
		defer wg.Done()
		credits, creditsErr = FetchResetCredits(ctx, client, accessToken, accountID, userAgent)
	}()
	wg.Wait()
	if usageErr != nil {
		return nil, usageErr
	}
	return BuildSnapshot(usage, credits, creditsErr, now), nil
}

// BuildSnapshot maps provider payloads into the stable representation consumed
// by applications. Reset-credit failures are non-fatal when usage is available.
func BuildSnapshot(usage *RateLimitStatusPayload, credits *ResetCreditsPayload, creditsErr error, now time.Time) *UsageSnapshot {
	snapshot := &UsageSnapshot{
		PlanType:        usage.PlanType,
		Windows:         []UsageWindow{},
		FetchedAtMillis: now.UnixMilli(),
	}
	if usage.RateLimit != nil {
		if usage.RateLimit.PrimaryWindow != nil {
			snapshot.Windows = append(snapshot.Windows, mapWindow(usage.RateLimit.PrimaryWindow, "short_window"))
		}
		if usage.RateLimit.SecondaryWindow != nil {
			secondary := mapWindow(usage.RateLimit.SecondaryWindow, "long_window")
			if len(snapshot.Windows) == 0 || snapshot.Windows[0].Kind != secondary.Kind {
				snapshot.Windows = append(snapshot.Windows, secondary)
			}
		}
	}
	if creditsErr != nil || credits == nil {
		snapshot.CreditsError = "Reset credits unavailable"
		return snapshot
	}

	metric := &CreditMetric{AvailableCount: credits.AvailableCount, TotalEarnedCount: credits.TotalEarnedCount}
	for _, credit := range credits.Credits {
		if !strings.EqualFold(credit.Status, "available") {
			continue
		}
		expiresAt, err := time.Parse(time.RFC3339, credit.ExpiresAt)
		if err != nil {
			continue
		}
		epoch := expiresAt.Unix()
		if metric.EarliestExpiryEpoch == nil || epoch < *metric.EarliestExpiryEpoch {
			metric.EarliestExpiryEpoch = &epoch
		}
	}
	snapshot.Credits = metric
	return snapshot
}

func WindowKind(seconds *int, fallback string) string {
	if seconds == nil {
		return fallback
	}
	if *seconds >= 1 && *seconds <= 86_400 {
		return "short_window"
	}
	return "long_window"
}

// WindowIsShort reports whether the window is the short, session-length window.
func WindowIsShort(window *RateLimitWindow, fallback bool) bool {
	if window == nil || window.LimitWindowSeconds == nil {
		return fallback
	}
	return WindowKind(window.LimitWindowSeconds, "long_window") == "short_window"
}

// SelectWindow returns the short window (session) when primary is true and the
// long window (weekly) otherwise, preferring the payload's primary window for
// the short kind and its secondary window for the long kind.
func SelectWindow(limits *RateLimitDetails, primary bool) *RateLimitWindow {
	if limits == nil {
		return nil
	}
	candidates := []struct {
		window        *RateLimitWindow
		fallbackShort bool
	}{
		{window: limits.PrimaryWindow, fallbackShort: true},
		{window: limits.SecondaryWindow, fallbackShort: false},
	}
	for _, candidate := range candidates {
		if candidate.window != nil && WindowIsShort(candidate.window, candidate.fallbackShort) == primary {
			return candidate.window
		}
	}
	return nil
}

// PercentValue clamps a usage percentage into the 0–100 range.
func PercentValue(usedPercent float64) float64 {
	value := usedPercent
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func mapWindow(window *RateLimitWindow, fallback string) UsageWindow {
	kind := WindowKind(window.LimitWindowSeconds, fallback)
	used := window.UsedPercent
	remaining := 100 - used
	if remaining < 0 {
		remaining = 0
	} else if remaining > 100 {
		remaining = 100
	}
	label := "5H"
	if kind == "long_window" {
		label = "7D"
	}
	return UsageWindow{
		Kind:          kind,
		Label:         label,
		UsedPercent:   &used,
		Remaining:     &remaining,
		ResetAt:       window.ResetAt,
		WindowSeconds: window.LimitWindowSeconds,
	}
}

func stringClaim(claims map[string]any, key string) string {
	value, _ := claims[key].(string)
	return strings.TrimSpace(value)
}

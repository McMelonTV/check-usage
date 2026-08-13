package codexapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestDefaultUserAgentUsesProjectName(t *testing.T) {
	if DefaultUserAgent != "check-usage/1.0.0" {
		t.Fatalf("user agent = %q", DefaultUserAgent)
	}
}

func TestParseIdentityReadsNestedAuthClaims(t *testing.T) {
	token := testJWT(t, map[string]any{
		"email": "person@example.com",
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_plan_type":  "pro",
			"chatgpt_account_id": "account-1",
		},
	})

	identity, err := ParseIdentity(token)
	if err != nil {
		t.Fatal(err)
	}
	if identity.Email != "person@example.com" || identity.PlanType != "pro" || identity.AccountID != "account-1" {
		t.Fatalf("unexpected identity: %#v", identity)
	}
}

func TestTokenExpiredOrNear(t *testing.T) {
	now := time.Unix(1_000, 0)
	near := testJWT(t, map[string]any{"exp": 1_059})
	fresh := testJWT(t, map[string]any{"exp": 1_061})
	if !TokenExpiredOrNear(near, now, time.Minute) {
		t.Fatal("expected near-expiry token to refresh")
	}
	if TokenExpiredOrNear(fresh, now, time.Minute) {
		t.Fatal("expected fresh token not to refresh")
	}
}

func TestWindowKindUsesDurationAndFallback(t *testing.T) {
	short, weekly := 18_000, 604_800
	for _, test := range []struct {
		seconds  *int
		fallback string
		want     string
	}{
		{&short, "long_window", "short_window"},
		{&weekly, "short_window", "long_window"},
		{nil, "short_window", "short_window"},
	} {
		if got := WindowKind(test.seconds, test.fallback); got != test.want {
			t.Fatalf("WindowKind(%v, %q) = %q, want %q", test.seconds, test.fallback, got, test.want)
		}
	}
}

func TestSelectWindowPrefersShortPrimaryAndLongSecondary(t *testing.T) {
	short, weekly := 18_000, 604_800
	primary := &RateLimitWindow{UsedPercent: 10, LimitWindowSeconds: &short, ResetAt: int64Ptr(1000)}
	secondary := &RateLimitWindow{UsedPercent: 20, LimitWindowSeconds: &weekly, ResetAt: int64Ptr(2000)}
	limits := &RateLimitDetails{PrimaryWindow: primary, SecondaryWindow: secondary}
	if got := SelectWindow(limits, true); got != primary {
		t.Fatalf("SelectWindow(primary=true) = %#v, want the short window", got)
	}
	if got := SelectWindow(limits, false); got != secondary {
		t.Fatalf("SelectWindow(primary=false) = %#v, want the long window", got)
	}
	// When the payload labels windows by position instead of duration, the
	// duration still decides the slot.
	swapped := &RateLimitDetails{PrimaryWindow: secondary, SecondaryWindow: primary}
	if got := SelectWindow(swapped, true); got != primary {
		t.Fatalf("SelectWindow swapped primary=true = %#v, want the short window", got)
	}
	// Missing windows fall back to the other candidate.
	onlySecondary := &RateLimitDetails{PrimaryWindow: nil, SecondaryWindow: secondary}
	if got := SelectWindow(onlySecondary, false); got != secondary {
		t.Fatalf("SelectWindow(onlySecondary) = %#v, want the long window", got)
	}
	if got := SelectWindow(nil, true); got != nil {
		t.Fatalf("SelectWindow(nil) = %#v, want nil", got)
	}
}

func int64Ptr(value int64) *int64 {
	return &value
}

func TestMobileErrorClassifications(t *testing.T) {
	invalidGrant := &HTTPError{Operation: "token refresh", StatusCode: http.StatusBadRequest}
	if !IsAuthenticationError(invalidGrant) {
		t.Fatal("invalid refresh grant should require authentication")
	}
	serverFailure := &HTTPError{Operation: "token refresh", StatusCode: http.StatusInternalServerError}
	if IsAuthenticationError(serverFailure) {
		t.Fatal("server failure should not require authentication")
	}
	decode := &DecodeError{Operation: "usage request", Err: errors.New("bad JSON")}
	if !IsCompatibilityError(decode) {
		t.Fatal("decode errors should be classified as compatibility errors")
	}
}

func testJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

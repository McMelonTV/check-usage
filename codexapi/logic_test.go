package codexapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"
)

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

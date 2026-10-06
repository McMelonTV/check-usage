package claudeapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestNewAuthSessionBuildsManualCodeURL(t *testing.T) {
	session, err := NewAuthSession()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(session.URL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if parsed.Scheme+"://"+parsed.Host+parsed.Path != AuthorizeURL || query.Get("code") != "true" || query.Get("client_id") != ClientID ||
		query.Get("redirect_uri") != ManualRedirectURI || query.Get("state") != session.State || query.Get("code_challenge_method") != "S256" ||
		query.Get("code_challenge") == "" || strings.Contains(session.URL, session.Verifier) {
		t.Fatalf("authorize URL = %s", session.URL)
	}
	parsedSession, err := ParseSessionID(session.SessionID())
	if err != nil || parsedSession.State != session.State || parsedSession.Verifier != session.Verifier {
		t.Fatalf("session round trip = %#v, %v", parsedSession, err)
	}
}

func TestCompleteLoginExchangesCodeAndReadsProfile(t *testing.T) {
	now := time.Unix(1000, 0)
	session := AuthSession{State: "state", Verifier: "verifier"}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.String() {
		case TokenURL:
			var body map[string]string
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["grant_type"] != "authorization_code" || body["code"] != "abc" || body["state"] != "state" || body["code_verifier"] != "verifier" || body["redirect_uri"] != ManualRedirectURI {
				t.Fatalf("token body = %#v", body)
			}
			return response(http.StatusOK, `{"access_token":"access","refresh_token":"refresh","expires_in":3600,"account":{"uuid":"uuid-1","email_address":"me@example.com"}}`), nil
		case ProfileURL:
			if request.Header.Get("Authorization") != "Bearer access" || request.Header.Get("anthropic-beta") != OAuthBeta {
				t.Fatalf("profile headers = %#v", request.Header)
			}
			return response(http.StatusOK, `{"organization":{"organization_type":"claude_max","rate_limit_tier":"default_claude_max_20x"}}`), nil
		}
		t.Fatalf("unexpected request %s", request.URL)
		return nil, nil
	})}
	login, err := CompleteLogin(t.Context(), client, session, " abc#state \n", now)
	if err != nil {
		t.Fatal(err)
	}
	want := Login{Credentials: Credentials{AccessToken: "access", RefreshToken: "refresh", ExpiresAt: 4600}, Email: "me@example.com", AccountUUID: "uuid-1", Plan: "Max 20x"}
	if login != want {
		t.Fatalf("login = %#v, want %#v", login, want)
	}
}

func TestCompleteLoginRejectsForeignState(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("no request expected")
		return nil, nil
	})}
	if _, err := CompleteLogin(t.Context(), client, AuthSession{State: "state", Verifier: "v"}, "abc#other", time.Now()); err == nil {
		t.Fatal("foreign state was accepted")
	}
}

func TestRefreshCredentialsOnlyWhenNearExpiry(t *testing.T) {
	now := time.Unix(10_000, 0)
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		var body map[string]string
		_ = json.NewDecoder(request.Body).Decode(&body)
		if body["grant_type"] != "refresh_token" || body["refresh_token"] != "old-refresh" {
			t.Fatalf("refresh body = %#v", body)
		}
		return response(http.StatusOK, `{"access_token":"new-access","expires_in":100}`), nil
	})}
	fresh := Credentials{AccessToken: "a", RefreshToken: "old-refresh", ExpiresAt: now.Unix() + 3600}
	if got, changed, err := RefreshCredentials(t.Context(), client, fresh, now); err != nil || changed || got != fresh || calls != 0 {
		t.Fatalf("fresh refresh = %#v, %v, %v, calls=%d", got, changed, err, calls)
	}
	stale := Credentials{AccessToken: "a", RefreshToken: "old-refresh", ExpiresAt: now.Unix() + 30}
	got, changed, err := RefreshCredentials(t.Context(), client, stale, now)
	want := Credentials{AccessToken: "new-access", RefreshToken: "old-refresh", ExpiresAt: now.Unix() + 100}
	if err != nil || !changed || got != want || calls != 1 {
		t.Fatalf("stale refresh = %#v, %v, %v, calls=%d", got, changed, err, calls)
	}
}

func TestRejectedRefreshRequiresSignIn(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusBadRequest, `{"error":"invalid_grant"}`), nil
	})}
	_, _, err := RefreshCredentials(t.Context(), client, Credentials{AccessToken: "a", RefreshToken: "r"}, time.Now())
	if !IsAuthenticationError(err) {
		t.Fatalf("error = %v, want authentication error", err)
	}
}

func TestOrganizationWithoutOAuthIsNotASignInError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusForbidden, `{"error":{"message":"OAuth not allowed","details":{"error_code":"oauth_not_allowed_for_organization"}}}`), nil
	})}
	_, err := FetchUsage(t.Context(), client, "token")
	if err != ErrUsageUnavailable || IsAuthenticationError(err) {
		t.Fatalf("error = %v", err)
	}
}

func TestRateLimitDelayHonorsRetryAfter(t *testing.T) {
	for _, tc := range []struct {
		header string
		want   time.Duration
	}{{"600", 10 * time.Minute}, {"", RateLimitBackoff}, {"30", RateLimitBackoff}} {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			resp := response(http.StatusTooManyRequests, `{"error":{"type":"rate_limit_error"}}`)
			if tc.header != "" {
				resp.Header.Set("Retry-After", tc.header)
			}
			return resp, nil
		})}
		_, err := FetchUsage(t.Context(), client, "token")
		if delay, limited := RateLimitDelay(err); !limited || delay != tc.want || IsAuthenticationError(err) {
			t.Fatalf("Retry-After %q: delay = %v, limited = %v, err = %v", tc.header, delay, limited, err)
		}
	}
}

func TestRequestsUseClaudeCodeUserAgent(t *testing.T) {
	seen := map[string]string{}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		seen[request.URL.String()] = request.Header.Get("User-Agent")
		switch request.URL.String() {
		case TokenURL:
			return response(http.StatusOK, `{"access_token":"a","refresh_token":"r","expires_in":60}`), nil
		case ProfileURL:
			return response(http.StatusOK, `{}`), nil
		}
		return response(http.StatusOK, `{}`), nil
	})}
	if _, err := CompleteLogin(t.Context(), client, AuthSession{State: "s", Verifier: "v"}, "code#s", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := FetchUsage(t.Context(), client, "a"); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{ProfileURL, UsageURL} {
		if seen[endpoint] != UserAgent || !strings.HasPrefix(UserAgent, "claude-code/") {
			t.Fatalf("%s User-Agent = %q", endpoint, seen[endpoint])
		}
	}
	// The token endpoint rate-limits the claude-code agent; it gets the HTTP
	// client's own agent, as Claude Code's requests do.
	if seen[TokenURL] != TokenUserAgent || strings.HasPrefix(seen[TokenURL], "claude-code/") {
		t.Fatalf("token User-Agent = %q", seen[TokenURL])
	}
}

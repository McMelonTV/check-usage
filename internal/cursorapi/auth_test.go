package cursorapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func TestBrowserLoginPKCEAndPolling(t *testing.T) {
	now := time.Now()
	session, err := BeginLogin(now)
	if err != nil {
		t.Fatal(err)
	}
	link, _ := url.Parse(session.LoginURL)
	digest := sha256.Sum256([]byte(session.Verifier))
	if link.Host != "cursor.com" || link.Query().Get("uuid") != session.ID || link.Query().Get("challenge") != base64.RawURLEncoding.EncodeToString(digest[:]) || strings.Contains(session.LoginURL, session.Verifier) {
		t.Fatal("invalid PKCE login")
	}
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != "/auth/poll" || r.URL.RawQuery != "" || r.Method != "POST" {
			t.Fatalf("poll leaked verifier in URL: %s", r.URL)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["uuid"] != session.ID || body["verifier"] != session.Verifier {
			t.Fatal("wrong handshake")
		}
		if calls == 1 {
			return response(404, `{}`), nil
		}
		return response(200, `{"accessToken":"access-secret","refreshToken":"discard-this"}`), nil
	})}
	if _, pending, err := PollLogin(t.Context(), client, session, now); err != nil || !pending {
		t.Fatalf("pending %v %v", pending, err)
	}
	if token, pending, err := PollLogin(t.Context(), client, session, now); err != nil || pending || token != "access-secret" {
		t.Fatalf("completion %v %v", pending, err)
	}
	if _, _, err := PollLogin(t.Context(), client, session, session.ExpiresAt); err == nil || calls != 2 {
		t.Fatal("expired login made request")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := PollLogin(ctx, &http.Client{}, session, now); err == nil {
		t.Fatal("cancelled login succeeded")
	}
}
func TestSDKKeyCreationAndRefresh(t *testing.T) {
	now := time.Now()
	fresh := "e30." + base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, now.Add(time.Hour).Unix()))) + ".sig"
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if strings.HasSuffix(r.URL.Path, "CreateUserApiKey") {
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["name"] != "check-usage" || body["expiresAt"] != fmt.Sprint(now.Add(90*24*time.Hour).UnixMilli()) || r.Header.Get("Authorization") != "Bearer access-secret" {
				t.Fatal("wrong key creation")
			}
			return response(200, `{"apiKey":"key-secret"}`), nil
		}
		if r.URL.Path != "/auth/exchange_user_api_key" || r.Header.Get("Authorization") != "Bearer key-secret" {
			t.Fatal("wrong refresh")
		}
		return response(200, fmt.Sprintf(`{"accessToken":%q}`, fresh)), nil
	})}
	key, err := CreateAPIKey(t.Context(), client, "access-secret", now)
	if err != nil || key != "key-secret" {
		t.Fatal(err)
	}
	token, err := AccessToken(t.Context(), client, key, "expired", now)
	if err != nil || token != fresh {
		t.Fatal(err)
	}
	if _, err := AccessToken(t.Context(), client, key, token, now); err != nil || calls != 2 {
		t.Fatal("fresh token exchanged again")
	}
	if _, err := AccessToken(t.Context(), client, "", token, now); !IsAuthenticationError(err) {
		t.Fatal("missing key accepted")
	}
	if TokenFresh(token, now.Add(56*time.Minute)) {
		t.Fatal("nearly expired token reused")
	}
}
func TestRequestsDoNotFollowRedirectsOrEchoSecrets(t *testing.T) {
	for _, status := range []int{401, 403, 302, 500} {
		calls := 0
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			r := response(status, `{"error":"key-secret"}`)
			r.Header.Set("Location", "https://example.com/secret")
			return r, nil
		})}
		_, err := ExchangeAPIKey(t.Context(), client, "key-secret")
		if err == nil || strings.Contains(err.Error(), "secret") || calls != 1 {
			t.Fatalf("unsafe response: %v, calls %d", err, calls)
		}
		if IsAuthenticationError(err) != (status == 401 || status == 403) {
			t.Fatalf("wrong error classification: %v", err)
		}
	}
}

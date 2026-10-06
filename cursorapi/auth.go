package cursorapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const LoginTimeout = 20 * time.Minute
const PollInterval = 2 * time.Second

type LoginSession struct {
	ID        string
	Verifier  string
	LoginURL  string
	ExpiresAt time.Time
}

func BeginLogin(now time.Time) (LoginSession, error) {
	var random [48]byte
	if _, err := rand.Read(random[:]); err != nil {
		return LoginSession{}, err
	}
	verifier := base64.RawURLEncoding.EncodeToString(random[:32])
	digest := sha256.Sum256([]byte(verifier))
	uuid := random[32:]
	uuid[6] = (uuid[6] & 0x0f) | 0x40
	uuid[8] = (uuid[8] & 0x3f) | 0x80
	id := fmt.Sprintf("%x-%x-%x-%x-%x", uuid[:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:])
	query := url.Values{"uuid": {id}, "challenge": {base64.RawURLEncoding.EncodeToString(digest[:])}, "mode": {"login"}, "redirectTarget": {"sdk"}}
	return LoginSession{ID: id, Verifier: verifier, LoginURL: "https://cursor.com/loginDeepControl?" + query.Encode(), ExpiresAt: now.Add(LoginTimeout)}, nil
}

// PollLogin keeps the PKCE verifier in the POST body, out of URLs and logs.
func PollLogin(ctx context.Context, client *http.Client, session LoginSession, now time.Time) (string, bool, error) {
	if !now.Before(session.ExpiresAt) {
		return "", false, fmt.Errorf("Cursor browser login expired; start login again")
	}
	if session.ID == "" || session.Verifier == "" {
		return "", false, fmt.Errorf("invalid Cursor browser login")
	}
	var tokens struct {
		AccessToken string `json:"accessToken"`
	}
	err := requestJSON(ctx, client, "/auth/poll", "", map[string]string{"uuid": session.ID, "verifier": session.Verifier}, &tokens)
	var httpErr *HTTPError
	if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound {
		return "", true, nil
	}
	if err != nil {
		return "", false, err
	}
	if tokens.AccessToken == "" {
		return "", false, fmt.Errorf("Cursor login response is missing an access token")
	}
	return tokens.AccessToken, false, nil
}

// CreateAPIKey follows Cursor.auth.login(): retain a revocable API key rather
// than broad browser credentials, and renew bearers by exchanging the key.
func CreateAPIKey(ctx context.Context, client *http.Client, token string, now time.Time) (string, error) {
	var result struct {
		APIKey string `json:"apiKey"`
	}
	err := rpc(ctx, client, token, "CreateUserApiKey", map[string]string{"name": "check-usage", "expiresAt": fmt.Sprint(now.Add(90 * 24 * time.Hour).UnixMilli())}, &result)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(result.APIKey) == "" {
		return "", fmt.Errorf("Cursor login returned no API key")
	}
	return result.APIKey, nil
}
func ExchangeAPIKey(ctx context.Context, client *http.Client, key string) (string, error) {
	if strings.TrimSpace(key) == "" {
		return "", ErrMissingAPIKey
	}
	var result struct {
		AccessToken string `json:"accessToken"`
	}
	if err := requestJSON(ctx, client, "/auth/exchange_user_api_key", strings.TrimSpace(key), struct{}{}, &result); err != nil {
		return "", err
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("Cursor API-key exchange returned no access token")
	}
	return result.AccessToken, nil
}
func TokenFresh(token string, now time.Time) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	return json.Unmarshal(data, &claims) == nil && claims.Exp > now.Add(5*time.Minute).Unix()
}
func AccessToken(ctx context.Context, client *http.Client, key, current string, now time.Time) (string, error) {
	if strings.TrimSpace(key) == "" {
		return "", ErrMissingAPIKey
	}
	if TokenFresh(current, now) {
		return current, nil
	}
	return ExchangeAPIKey(ctx, client, key)
}

package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func ensureFreshTokens(acc storedAccount, client *http.Client) (storedAccount, bool, error) {
	if acc.AuthData.AccessToken == nil || acc.AuthData.RefreshToken == nil {
		return acc, false, fmt.Errorf("missing access/refresh token")
	}
	if !tokenExpiredOrNear(*acc.AuthData.AccessToken) {
		return acc, false, nil
	}

	v := url.Values{}
	v.Set("grant_type", "refresh_token")
	v.Set("refresh_token", *acc.AuthData.RefreshToken)
	v.Set("client_id", oauthClientID)
	req, err := http.NewRequest(http.MethodPost, oauthTokenURL, strings.NewReader(v.Encode()))
	if err != nil {
		return acc, false, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return acc, false, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return acc, false, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return acc, false, fmt.Errorf("refresh failed: %s: %s", resp.Status, truncateText(string(body), 300))
	}
	var r struct {
		IDToken      *string `json:"id_token"`
		AccessToken  string  `json:"access_token"`
		RefreshToken *string `json:"refresh_token"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return acc, false, err
	}

	acc.AuthData.AccessToken = &r.AccessToken
	if r.IDToken != nil {
		acc.AuthData.IDToken = r.IDToken
	}
	if r.RefreshToken != nil {
		acc.AuthData.RefreshToken = r.RefreshToken
	}
	return acc, true, nil
}

func fetchUsage(acc storedAccount, client *http.Client) (*rateLimitStatusPayload, error) {
	if acc.AuthData.AccessToken == nil {
		return nil, fmt.Errorf("missing access token")
	}
	req, err := http.NewRequest(http.MethodGet, chatgptUsageURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+*acc.AuthData.AccessToken)
	req.Header.Set("User-Agent", userAgent)
	if acc.AuthData.AccountID != nil && *acc.AuthData.AccountID != "" {
		req.Header.Set("chatgpt-account-id", *acc.AuthData.AccountID)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("usage request failed: %s: %s", resp.Status, truncateText(string(body), 300))
	}

	var payload rateLimitStatusPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

func tokenExpiredOrNear(token string) bool {
	exp, ok := jwtExp(token)
	if !ok {
		return true
	}
	return exp <= time.Now().Unix()+expirySkew
}

func jwtExp(token string) (int64, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return 0, false
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0, false
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return 0, false
	}
	switch v := m["exp"].(type) {
	case float64:
		return int64(v), true
	case string:
		n, err := strconv.ParseInt(v, 10, 64)
		return n, err == nil && n != 0
	default:
		return 0, false
	}
}

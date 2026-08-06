package codexapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	ClientID              = "app_EMoamEEZ73f0CkXaXp7hrann"
	TokenURL              = "https://auth.openai.com/oauth/token"
	DeviceAuthBaseURL     = "https://auth.openai.com/api/accounts/deviceauth"
	DeviceVerificationURL = "https://auth.openai.com/codex/device"
	DeviceRedirectURI     = "https://auth.openai.com/deviceauth/callback"
	UsageURL              = "https://chatgpt.com/backend-api/wham/usage"
	ResetCreditsURL       = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits"
	DefaultUserAgent      = "codex-usage/1.0.0"
)

type HTTPError struct {
	Operation  string
	StatusCode int
	Status     string
	Body       string
}

type DecodeError struct {
	Operation string
	Err       error
}

func (e *DecodeError) Error() string {
	return fmt.Sprintf("decode %s response: %v", e.Operation, e.Err)
}
func (e *DecodeError) Unwrap() error { return e.Err }

func (e *HTTPError) Error() string {
	return fmt.Sprintf("%s failed: %s: %s", e.Operation, e.Status, e.Body)
}

func IsAuthenticationError(err error) bool {
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		return false
	}
	return httpErr.StatusCode == http.StatusUnauthorized ||
		(httpErr.Operation == "token refresh" && httpErr.StatusCode >= 400 && httpErr.StatusCode < 500)
}

func IsCompatibilityError(err error) bool {
	var decodeErr *DecodeError
	return errors.As(err, &decodeErr)
}

func RequestDeviceUserCode(ctx context.Context, client *http.Client) (*DeviceUserCodeResponse, error) {
	body, err := json.Marshal(map[string]string{"client_id": ClientID})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, DeviceAuthBaseURL+"/usercode", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	var out DeviceUserCodeResponse
	if err := doJSON(client, req, "device auth usercode request", &out); err != nil {
		return nil, err
	}
	if strings.TrimSpace(out.DeviceAuthID) == "" || strings.TrimSpace(out.UserCode) == "" {
		return nil, fmt.Errorf("device auth response missing required fields")
	}
	if strings.TrimSpace(out.Interval) == "" {
		out.Interval = "5"
	}
	return &out, nil
}

// PollDeviceToken performs one poll. Pending is true when authorization has not
// yet been completed by the user.
func PollDeviceToken(ctx context.Context, client *http.Client, deviceAuthID, userCode string) (result *DeviceTokenPollResponse, pending bool, err error) {
	body, err := json.Marshal(map[string]string{"device_auth_id": deviceAuthID, "user_code": userCode})
	if err != nil {
		return nil, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, DeviceAuthBaseURL+"/token", strings.NewReader(string(body)))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, err
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
		return nil, true, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false, responseError("device auth polling", resp, responseBody)
	}

	var out DeviceTokenPollResponse
	if err := json.Unmarshal(responseBody, &out); err != nil {
		return nil, false, &DecodeError{Operation: "device auth polling", Err: err}
	}
	if strings.TrimSpace(out.AuthorizationCode) == "" || strings.TrimSpace(out.CodeVerifier) == "" {
		return nil, false, fmt.Errorf("device auth token response missing authorization code or code verifier")
	}
	return &out, false, nil
}

func ExchangeAuthorizationCode(ctx context.Context, client *http.Client, code, redirectURI, codeVerifier string) (*TokenResponse, error) {
	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("code", code)
	values.Set("redirect_uri", redirectURI)
	values.Set("client_id", ClientID)
	values.Set("code_verifier", codeVerifier)
	return requestTokens(ctx, client, values, "token exchange")
}

func RefreshTokens(ctx context.Context, client *http.Client, refreshToken string) (*TokenResponse, error) {
	values := url.Values{}
	values.Set("grant_type", "refresh_token")
	values.Set("refresh_token", refreshToken)
	values.Set("client_id", ClientID)
	return requestTokens(ctx, client, values, "token refresh")
}

func FetchUsage(ctx context.Context, client *http.Client, accessToken, accountID, userAgent string) (*RateLimitStatusPayload, error) {
	var out RateLimitStatusPayload
	if err := getAuthenticated(ctx, client, UsageURL, accessToken, accountID, userAgent, false, "usage request", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func FetchResetCredits(ctx context.Context, client *http.Client, accessToken, accountID, userAgent string) (*ResetCreditsPayload, error) {
	var out ResetCreditsPayload
	if err := getAuthenticated(ctx, client, ResetCreditsURL, accessToken, accountID, userAgent, true, "reset credits request", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func requestTokens(ctx context.Context, client *http.Client, values url.Values, operation string) (*TokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, TokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var out TokenResponse
	if err := doJSON(client, req, operation, &out); err != nil {
		return nil, err
	}
	if strings.TrimSpace(out.AccessToken) == "" {
		return nil, fmt.Errorf("%s response missing access token", operation)
	}
	return &out, nil
}

func getAuthenticated(ctx context.Context, client *http.Client, endpoint, accessToken, accountID, userAgent string, resetCredits bool, operation string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	if strings.TrimSpace(userAgent) == "" {
		userAgent = DefaultUserAgent
	}
	req.Header.Set("User-Agent", userAgent)
	if strings.TrimSpace(accountID) != "" {
		req.Header.Set("chatgpt-account-id", accountID)
	}
	if resetCredits {
		req.Header.Set("OpenAI-Beta", "codex-1")
		req.Header.Set("originator", "Codex Desktop")
	}
	return doJSON(client, req, operation, out)
}

func doJSON(client *http.Client, req *http.Request, operation string, out any) error {
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return responseError(operation, resp, body)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return &DecodeError{Operation: operation, Err: err}
	}
	return nil
}

func responseError(operation string, resp *http.Response, body []byte) error {
	text := strings.TrimSpace(string(body))
	if len(text) > 300 {
		text = text[:300] + "..."
	}
	return &HTTPError{Operation: operation, StatusCode: resp.StatusCode, Status: resp.Status, Body: text}
}

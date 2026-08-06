package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const oauthCallbackPath = "/auth/callback"

type oauthTokenResponse struct {
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type pkceCodes struct {
	Verifier  string
	Challenge string
}

type oauthResult struct {
	Account storedAccount
	Err     error
}

type deviceUserCodeResponse struct {
	DeviceAuthID string `json:"device_auth_id"`
	UserCode     string `json:"user_code"`
	Interval     string `json:"interval"`
}

type deviceTokenPollResponse struct {
	AuthorizationCode string `json:"authorization_code"`
	CodeChallenge     string `json:"code_challenge"`
	CodeVerifier      string `json:"code_verifier"`
}

func runOAuthLogin(accountName string, client *http.Client, openBrowser bool) (storedAccount, error) {
	pkce, err := generatePKCE()
	if err != nil {
		return storedAccount{}, err
	}
	state, err := generateState()
	if err != nil {
		return storedAccount{}, err
	}

	listener, port, err := listenOAuthCallback()
	if err != nil {
		return storedAccount{}, err
	}

	redirectURI := fmt.Sprintf("http://localhost:%d%s", port, oauthCallbackPath)
	authURL := buildAuthorizeURL(redirectURI, pkce.Challenge, state)

	resultCh := make(chan oauthResult, 1)
	once := &sync.Once{}
	serveMux := http.NewServeMux()
	serveMux.HandleFunc(oauthCallbackPath, func(w http.ResponseWriter, r *http.Request) {
		handleOAuthCallback(w, r, accountName, redirectURI, state, pkce.Verifier, client, resultCh, once)
	})
	serveMux.HandleFunc("/cancel", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("login cancelled"))
		once.Do(func() { resultCh <- oauthResult{Err: fmt.Errorf("login cancelled")} })
	})

	server := &http.Server{Handler: serveMux}
	go func() {
		_ = server.Serve(listener)
	}()

	fmt.Printf("Open this URL to continue login:\n%s\n", authURL)
	if openBrowser {
		if err := openBrowserURL(authURL); err != nil {
			fmt.Printf("Could not open browser automatically: %v\n", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(oauthTimeout)*time.Second)
	defer cancel()

	select {
	case result := <-resultCh:
		_ = server.Shutdown(context.Background())
		return result.Account, result.Err
	case <-ctx.Done():
		_ = server.Shutdown(context.Background())
		return storedAccount{}, fmt.Errorf("oauth login timed out after %d seconds", oauthTimeout)
	}
}

func handleOAuthCallback(
	w http.ResponseWriter,
	r *http.Request,
	requestedName string,
	redirectURI string,
	expectedState string,
	codeVerifier string,
	client *http.Client,
	resultCh chan<- oauthResult,
	once *sync.Once,
) {
	q := r.URL.Query()
	if oauthErr := q.Get("error"); oauthErr != "" {
		desc := q.Get("error_description")
		msg := fmt.Sprintf("oauth error: %s", oauthErr)
		if strings.TrimSpace(desc) != "" {
			msg = msg + ": " + desc
		}
		http.Error(w, msg, http.StatusBadRequest)
		once.Do(func() { resultCh <- oauthResult{Err: fmt.Errorf("%s", msg)} })
		return
	}

	if q.Get("state") != expectedState {
		http.Error(w, "oauth state mismatch", http.StatusBadRequest)
		once.Do(func() { resultCh <- oauthResult{Err: fmt.Errorf("oauth state mismatch")} })
		return
	}

	code := q.Get("code")
	if strings.TrimSpace(code) == "" {
		http.Error(w, "missing authorization code", http.StatusBadRequest)
		once.Do(func() { resultCh <- oauthResult{Err: fmt.Errorf("missing authorization code")} })
		return
	}

	tokens, err := exchangeAuthorizationCode(client, code, redirectURI, codeVerifier)
	if err != nil {
		http.Error(w, "token exchange failed", http.StatusInternalServerError)
		once.Do(func() { resultCh <- oauthResult{Err: err} })
		return
	}

	email, planType, accountID := parseIDTokenClaims(tokens.IDToken)
	account := buildStoredAccount(requestedName, email, planType, accountID, tokens)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte("<!doctype html><html><body><h1>Login successful</h1><p>You can close this tab.</p></body></html>"))
	once.Do(func() { resultCh <- oauthResult{Account: account} })
}

func runDeviceAuthLogin(accountName string, client *http.Client, openBrowser bool) (storedAccount, error) {
	deviceCode, err := requestDeviceUserCode(client)
	if err != nil {
		return storedAccount{}, err
	}

	printDeviceCodePrompt(deviceCode.UserCode)
	if openBrowser {
		if err := openBrowserURL(deviceAuthVerificationURL); err != nil {
			fmt.Printf("Could not open browser automatically: %v\n", err)
		}
	}

	pollResp, err := pollDeviceToken(client, deviceCode)
	if err != nil {
		return storedAccount{}, err
	}

	tokens, err := exchangeAuthorizationCode(client, pollResp.AuthorizationCode, deviceAuthRedirectURI, pollResp.CodeVerifier)
	if err != nil {
		return storedAccount{}, fmt.Errorf("device auth token exchange failed: %w", err)
	}

	email, planType, accountID := parseIDTokenClaims(tokens.IDToken)
	return buildStoredAccount(accountName, email, planType, accountID, tokens), nil
}

func requestDeviceUserCode(client *http.Client) (*deviceUserCodeResponse, error) {
	body := `{"client_id":"` + oauthClientID + `"}`
	req, err := http.NewRequest(http.MethodPost, deviceAuthBaseURL+deviceAuthUserCodePath, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("device auth usercode request failed: %s: %s", resp.Status, truncateText(string(bodyBytes), 300))
	}

	var out deviceUserCodeResponse
	if err := json.Unmarshal(bodyBytes, &out); err != nil {
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

func pollDeviceToken(client *http.Client, code *deviceUserCodeResponse) (*deviceTokenPollResponse, error) {
	intervalSeconds := parseIntervalSeconds(code.Interval)
	deadline := time.Now().Add(time.Duration(deviceAuthMaxWaitSeconds) * time.Second)
	url := deviceAuthBaseURL + deviceAuthTokenPath

	for {
		reqBody := `{"device_auth_id":"` + jsonEscape(code.DeviceAuthID) + `","user_code":"` + jsonEscape(code.UserCode) + `"}`
		req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(reqBody))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		bodyBytes, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			var out deviceTokenPollResponse
			if err := json.Unmarshal(bodyBytes, &out); err != nil {
				return nil, err
			}
			if strings.TrimSpace(out.AuthorizationCode) == "" || strings.TrimSpace(out.CodeVerifier) == "" {
				return nil, fmt.Errorf("device auth token response missing authorization code or code verifier")
			}
			return &out, nil
		}

		if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusNotFound {
			return nil, fmt.Errorf("device auth polling failed: %s: %s", resp.Status, truncateText(string(bodyBytes), 300))
		}

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("device auth timed out after %d seconds", deviceAuthMaxWaitSeconds)
		}

		time.Sleep(time.Duration(intervalSeconds) * time.Second)
	}
}

func parseIntervalSeconds(v string) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return 5
	}
	return n
}

func printDeviceCodePrompt(userCode string) {
	fmt.Printf("\nDevice auth login:\n")
	fmt.Printf("1) Open: %s\n", deviceAuthVerificationURL)
	fmt.Printf("2) Enter code: %s\n\n", userCode)
}

func buildStoredAccount(requestedName string, email, planType, accountID *string, tokens *oauthTokenResponse) storedAccount {
	name := strings.TrimSpace(requestedName)
	if name == "" {
		name = defaultAccountName(email)
	}

	return storedAccount{
		ID:       newAccountID(),
		Name:     name,
		Provider: "OpenAI",
		Email:    email,
		PlanType: planType,
		AuthData: authData{
			Type:         "chatgpt",
			IDToken:      strPtr(tokens.IDToken),
			AccessToken:  strPtr(tokens.AccessToken),
			RefreshToken: strPtr(tokens.RefreshToken),
			AccountID:    accountID,
		},
	}
}

func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	if len(b) >= 2 {
		return string(b[1 : len(b)-1])
	}
	return s
}

func listenOAuthCallback() (net.Listener, int, error) {
	defaultAddr := fmt.Sprintf("127.0.0.1:%d", oauthDefaultPort)
	listener, err := net.Listen("tcp", defaultAddr)
	if err == nil {
		return listener, oauthDefaultPort, nil
	}

	if strings.Contains(strings.ToLower(err.Error()), "address already in use") {
		_ = sendCancelRequest(oauthDefaultPort)
		time.Sleep(250 * time.Millisecond)
		listener, retryErr := net.Listen("tcp", defaultAddr)
		if retryErr == nil {
			return listener, oauthDefaultPort, nil
		}
		err = retryErr
	}

	fallback, fallbackErr := net.Listen("tcp", "127.0.0.1:0")
	if fallbackErr != nil {
		return nil, 0, fmt.Errorf(
			"failed to bind oauth callback on %s (%v) and fallback port (%v)",
			defaultAddr,
			err,
			fallbackErr,
		)
	}
	port := fallback.Addr().(*net.TCPAddr).Port
	fmt.Printf("Callback port %d unavailable, using %d\n", oauthDefaultPort, port)
	return fallback, port, nil
}

func sendCancelRequest(port int) error {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	_, err = io.WriteString(conn, "GET /cancel HTTP/1.1\r\nHost: "+addr+"\r\nConnection: close\r\n\r\n")
	if err != nil {
		return err
	}
	buf := make([]byte, 64)
	_, _ = conn.Read(buf)
	return nil
}

func buildAuthorizeURL(redirectURI, challenge, state string) string {
	params := [][2]string{
		{"response_type", "code"},
		{"client_id", oauthClientID},
		{"redirect_uri", redirectURI},
		{"scope", oauthScope},
		{"code_challenge", challenge},
		{"code_challenge_method", "S256"},
		{"id_token_add_organizations", "true"},
		{"codex_cli_simplified_flow", "true"},
		{"state", state},
		{"originator", oauthOriginator},
	}

	parts := make([]string, 0, len(params))
	for _, p := range params {
		parts = append(parts, p[0]+"="+oauthEncode(p[1]))
	}

	return fmt.Sprintf("%s/oauth/authorize?%s", oauthIssuerURL, strings.Join(parts, "&"))
}

func exchangeAuthorizationCode(client *http.Client, code, redirectURI, codeVerifier string) (*oauthTokenResponse, error) {
	reqBody := "grant_type=authorization_code" +
		"&code=" + oauthEncode(code) +
		"&redirect_uri=" + oauthEncode(redirectURI) +
		"&client_id=" + oauthEncode(oauthClientID) +
		"&code_verifier=" + oauthEncode(codeVerifier)

	req, err := http.NewRequest(http.MethodPost, oauthTokenURL, strings.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

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
		return nil, fmt.Errorf("token exchange failed: %s: %s", resp.Status, truncateText(string(body), 300))
	}

	var tokens oauthTokenResponse
	if err := json.Unmarshal(body, &tokens); err != nil {
		return nil, err
	}
	if strings.TrimSpace(tokens.AccessToken) == "" || strings.TrimSpace(tokens.RefreshToken) == "" || strings.TrimSpace(tokens.IDToken) == "" {
		return nil, fmt.Errorf("token exchange response missing required tokens")
	}
	return &tokens, nil
}

func oauthEncode(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

func parseIDTokenClaims(idToken string) (*string, *string, *string) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil, nil, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, nil, nil
	}

	var m map[string]any
	if err := json.Unmarshal(payload, &m); err != nil {
		return nil, nil, nil
	}

	email := mapStringPtr(m, "email")
	authClaim, _ := m["https://api.openai.com/auth"].(map[string]any)
	planType := mapStringPtr(authClaim, "chatgpt_plan_type")
	accountID := mapStringPtr(authClaim, "chatgpt_account_id")
	return email, planType, accountID
}

func mapStringPtr(m map[string]any, key string) *string {
	if m == nil {
		return nil
	}
	v, ok := m[key].(string)
	if !ok || strings.TrimSpace(v) == "" {
		return nil
	}
	return strPtr(v)
}

func generatePKCE() (*pkceCodes, error) {
	b := make([]byte, 64)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	verifier := base64.RawURLEncoding.EncodeToString(b)
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	return &pkceCodes{Verifier: verifier, Challenge: challenge}, nil
}

func generateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func newAccountID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("acc-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func defaultAccountName(email *string) string {
	if email != nil {
		e := strings.TrimSpace(*email)
		if e != "" {
			if at := strings.Index(e, "@"); at > 0 {
				return e[:at]
			}
			return e
		}
	}
	return fmt.Sprintf("account-%d", time.Now().Unix())
}

func openBrowserURL(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	return cmd.Start()
}

func strPtr(s string) *string {
	v := s
	return &v
}

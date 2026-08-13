package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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

	"github.com/McMelonTV/check-usage/codexapi"
)

const oauthCallbackPath = "/auth/callback"

type oauthTokenResponse = codexapi.TokenResponse

type pkceCodes struct {
	Verifier  string
	Challenge string
}

type oauthResult struct {
	Account storedAccount
	Err     error
}

type deviceUserCodeResponse = codexapi.DeviceUserCodeResponse
type deviceTokenPollResponse = codexapi.DeviceTokenPollResponse

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
	return codexapi.RequestDeviceUserCode(context.Background(), client)
}

func pollDeviceToken(client *http.Client, code *deviceUserCodeResponse) (*deviceTokenPollResponse, error) {
	intervalSeconds := parseIntervalSeconds(code.Interval)
	deadline := time.Now().Add(time.Duration(deviceAuthMaxWaitSeconds) * time.Second)
	for {
		out, pending, err := codexapi.PollDeviceToken(context.Background(), client, code.DeviceAuthID, code.UserCode)
		if err != nil {
			return nil, err
		}
		if !pending {
			return out, nil
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
		Provider: providerCodex,
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
	tokens, err := codexapi.ExchangeAuthorizationCode(context.Background(), client, code, redirectURI, codeVerifier)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(tokens.RefreshToken) == "" || strings.TrimSpace(tokens.IDToken) == "" {
		return nil, fmt.Errorf("token exchange response missing required tokens")
	}
	return tokens, nil
}

func oauthEncode(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

func parseIDTokenClaims(idToken string) (*string, *string, *string) {
	identity, err := codexapi.ParseIdentity(idToken)
	if err != nil {
		return nil, nil, nil
	}
	return optionalString(identity.Email), optionalString(identity.PlanType), optionalString(identity.AccountID)
}

func optionalString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strPtr(value)
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

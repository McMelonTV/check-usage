package nativecli

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// agy has no print-mode command that reports the plan. Its interactive
// process runs a loopback language server whose GetUserStatus RPC does, and
// that server accepts a CSRF token supplied through ANTIGRAVITY_SERVER_CSRF_TOKEN.
// The plan probe starts agy with a fresh token, asks that server once, and
// stops agy again. agy itself performs the upstream request with its own login.
const (
	agyCSRFEnv       = "ANTIGRAVITY_SERVER_CSRF_TOKEN"
	agyCSRFHeader    = "X-Codeium-Csrf-Token"
	agyUserStatusRPC = "/exa.language_server_pb.LanguageServerService/GetUserStatus"
	agyPlanTimeout   = 30 * time.Second
)

// AgyPlanDue limits plan probes, which briefly start interactive agy: daily
// once a plan is known, hourly while it is still unknown.
func AgyPlanDue(plan string, lastChecked, now time.Time) bool {
	if plan == "" {
		return now.Sub(lastChecked) >= time.Hour
	}
	return now.Sub(lastChecked) >= 24*time.Hour
}

var errAgySignedOut = errors.New("agy is not signed in")

func newCSRFToken() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

// loopbackClient talks only to 127.0.0.1. The language server uses a
// self-signed certificate, so verification is skipped for loopback alone.
func loopbackClient() *http.Client {
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	return &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, _, err := net.SplitHostPort(address)
			if err != nil || host != "127.0.0.1" {
				return nil, errors.New("agy status client only dials 127.0.0.1")
			}
			return dialer.DialContext(ctx, network, address)
		},
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}}
}

// pollAgyPlan retries every listening port until GetUserStatus reports a plan,
// reports that agy is signed out, or the context ends.
func pollAgyPlan(ctx context.Context, ports func() []int, token string) (string, error) {
	client := loopbackClient()
	defer client.CloseIdleConnections()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		for _, port := range ports() {
			plan, err := queryAgyPlan(ctx, client, port, token)
			if err == nil && plan != "" || errors.Is(err, errAgySignedOut) {
				return plan, err
			}
		}
		select {
		case <-ctx.Done():
			return "", errors.New("agy status server did not report a plan in time")
		case <-ticker.C:
		}
	}
}

func queryAgyPlan(ctx context.Context, client *http.Client, port int, token string) (string, error) {
	body := []byte(`{"metadata":{"ideName":"antigravity","extensionName":"antigravity","locale":"en","ideVersion":"unknown"}}`)
	for _, scheme := range []string{"https", "http"} {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, scheme+"://127.0.0.1:"+strconv.Itoa(port)+agyUserStatusRPC, bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Connect-Protocol-Version", "1")
		request.Header.Set(agyCSRFHeader, token)
		response, err := client.Do(request)
		if err != nil {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		response.Body.Close()
		if err != nil || response.StatusCode != http.StatusOK {
			return "", fmt.Errorf("agy status server returned HTTP %d", response.StatusCode)
		}
		return parseAgyPlan(data)
	}
	return "", errors.New("agy status server is not reachable")
}

func parseAgyPlan(data []byte) (string, error) {
	type planInfo struct {
		PlanName string `json:"planName"`
	}
	var status struct {
		UserStatus *struct {
			PlanStatus *struct {
				PlanInfo *planInfo `json:"planInfo"`
			} `json:"planStatus"`
			PlanInfo               *planInfo       `json:"planInfo"`
			UserTier               json.RawMessage `json:"userTier"`
			G1Tier                 json.RawMessage `json:"g1Tier"`
			CascadeModelConfigData *struct {
				ErrorMessage string `json:"errorMessage"`
			} `json:"cascadeModelConfigData"`
		} `json:"userStatus"`
	}
	if json.Unmarshal(data, &status) != nil || status.UserStatus == nil {
		return "", errors.New("agy returned invalid user status")
	}
	user := status.UserStatus
	if user.PlanStatus != nil && user.PlanStatus.PlanInfo != nil {
		if plan := strings.TrimSpace(user.PlanStatus.PlanInfo.PlanName); plan != "" {
			return plan, nil
		}
	}
	if user.PlanInfo != nil {
		if plan := strings.TrimSpace(user.PlanInfo.PlanName); plan != "" {
			return plan, nil
		}
	}
	var tier struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(user.UserTier, &tier) == nil && strings.TrimSpace(tier.Name) != "" {
		return strings.TrimSpace(tier.Name), nil
	}
	var g1 string
	if json.Unmarshal(user.G1Tier, &g1) == nil {
		if plan := g1TierName(g1); plan != "" {
			return plan, nil
		}
	}
	if user.CascadeModelConfigData != nil && strings.Contains(strings.ToLower(user.CascadeModelConfigData.ErrorMessage), "not logged in") {
		return "", errAgySignedOut
	}
	return "", nil
}

// g1TierName turns enum names such as G1_ULTRA_LITE_TIER into "Ultra Lite".
func g1TierName(value string) string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "G1_") || !strings.HasSuffix(value, "_TIER") || len(value) <= len("G1__TIER") {
		return ""
	}
	words := strings.Split(strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(value, "G1_"), "_TIER")), "_")
	for i, word := range words {
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

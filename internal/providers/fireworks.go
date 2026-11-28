package providers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// fireworksAPIBase is the public Fireworks gateway. Nexus plan usage comes from
// the same gateway as the platform API, but under the Nexus account's own key.
const fireworksAPIBase = "https://api.fireworks.ai"

// fireworksWindow is one included-usage window on a Nexus seat.
type fireworksWindow struct {
	UsedPercent *float64 `json:"usedPercent"`
	EndTime     string   `json:"endTime"`
}

type fireworksPlan struct {
	Tier  string `json:"tier"`
	State string `json:"state"`
}

// fetchFireworksNexus reads a Fireworks Nexus subscription's included usage.
// Pro and Team seats report a 5-hour rolling session window and a monthly
// window; Enterprise bills by usage instead and only reports its plan.
func fetchFireworksNexus(ctx context.Context, client *http.Client, key, userAgent string) (Usage, error) {
	accountID, email, err := verifyFireworksKey(ctx, client, key, userAgent)
	if err != nil {
		return Usage{}, err
	}
	plan, err := fetchFireworksPlan(ctx, client, key, userAgent, accountID)
	if err != nil {
		return Usage{}, err
	}
	usage := Usage{Plan: fireworksTierName(plan.Tier)}
	if !fireworksSeatTier(plan.Tier) {
		return usage, nil
	}
	user, err := fetchFireworksUser(ctx, client, key, userAgent, accountID, email)
	if err != nil {
		return Usage{}, err
	}
	return fetchFireworksSeatUsage(ctx, client, key, userAgent, user, usage)
}

// verifyFireworksKey checks the key and returns the account id and developer
// email the gateway associates with it. The gateway answers with identity
// headers and an empty body.
func verifyFireworksKey(ctx context.Context, client *http.Client, key, userAgent string) (string, string, error) {
	request, err := providerRequest(ctx, fireworksAPIBase+"/verifyApiKey", key, userAgent)
	if err != nil {
		return "", "", err
	}
	response, err := client.Do(request)
	if err != nil {
		return "", "", err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 8*1024))
	if err != nil {
		return "", "", err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", "", &HTTPError{Operation: "Fireworks key verification", StatusCode: response.StatusCode, Status: response.Status, Body: httpErrorBody(body)}
	}
	accountID := strings.TrimPrefix(strings.TrimSpace(response.Header.Get("x-fireworks-account-id")), "accounts/")
	if accountID == "" {
		return "", "", fmt.Errorf("Fireworks key verification returned no account id")
	}
	return accountID, strings.TrimSpace(response.Header.Get("x-fireworks-developer-email")), nil
}

func fetchFireworksPlan(ctx context.Context, client *http.Client, key, userAgent, accountID string) (fireworksPlan, error) {
	endpoint := fmt.Sprintf("%s/v1/accounts/%s/nexusPlans?pageSize=10", fireworksAPIBase, url.PathEscape(accountID))
	request, err := providerRequest(ctx, endpoint, key, userAgent)
	if err != nil {
		return fireworksPlan{}, err
	}
	var payload struct {
		Plans []fireworksPlan `json:"nexusPlans"`
	}
	if err := doJSON(client, request, "Fireworks Nexus plan request", &payload); err != nil {
		return fireworksPlan{}, err
	}
	for _, plan := range payload.Plans {
		// An account has at most one plan that has not ended.
		if strings.TrimSpace(plan.Tier) != "" && plan.State != "" && !strings.EqualFold(plan.State, "ENDED") {
			return plan, nil
		}
	}
	return fireworksPlan{}, fmt.Errorf("Fireworks account has no active Nexus plan")
}

// fetchFireworksUser finds the resource name of the signed-in user, which the
// seat usage endpoint needs. Service-account keys carry no developer email and
// cannot resolve to a seat.
func fetchFireworksUser(ctx context.Context, client *http.Client, key, userAgent, accountID, email string) (string, error) {
	if email == "" {
		return "", fmt.Errorf("Fireworks Nexus needs a user sign-in key; service-account keys have no plan usage")
	}
	query := url.Values{
		"filter":   {fmt.Sprintf("email=%q", email)},
		"pageSize": {"2"},
	}
	endpoint := fmt.Sprintf("%s/v1/accounts/%s/users?%s", fireworksAPIBase, url.PathEscape(accountID), query.Encode())
	request, err := providerRequest(ctx, endpoint, key, userAgent)
	if err != nil {
		return "", err
	}
	var payload struct {
		Users []struct {
			Name string `json:"name"`
		} `json:"users"`
	}
	if err := doJSON(client, request, "Fireworks Nexus user request", &payload); err != nil {
		return "", err
	}
	parent := "accounts/" + accountID + "/users/"
	for _, user := range payload.Users {
		rest := strings.TrimPrefix(user.Name, parent)
		// Keep provider data from steering the follow-up request elsewhere.
		if rest == "" || rest == user.Name || strings.ContainsAny(rest, "/?#") {
			continue
		}
		return user.Name, nil
	}
	return "", fmt.Errorf("Fireworks Nexus could not find the signed-in user in this account")
}

func fetchFireworksSeatUsage(ctx context.Context, client *http.Client, key, userAgent, user string, usage Usage) (Usage, error) {
	request, err := providerRequest(ctx, fmt.Sprintf("%s/v1/%s/nexusPlanUsage", fireworksAPIBase, user), key, userAgent)
	if err != nil {
		return Usage{}, err
	}
	var payload struct {
		Monthly       *fireworksWindow `json:"monthly"`
		RollingWindow *fireworksWindow `json:"rollingWindow"`
	}
	if err := doJSON(client, request, "Fireworks Nexus usage request", &payload); err != nil {
		return Usage{}, err
	}
	if payload.Monthly == nil {
		return Usage{}, fmt.Errorf("Fireworks Nexus usage is missing the monthly window")
	}
	metrics := make([]Metric, 0, 2)
	if payload.RollingWindow != nil {
		session, err := payload.RollingWindow.metric(SessionSlot, "SESSION", "session")
		if err != nil {
			return Usage{}, err
		}
		metrics = append(metrics, session)
	}
	monthly, err := payload.Monthly.metric(MonthlySlot, "MONTHLY", "monthly")
	if err != nil {
		return Usage{}, err
	}
	usage.Metrics = append(metrics, monthly)
	return usage, nil
}

func (window *fireworksWindow) metric(slot MetricSlot, label, name string) (Metric, error) {
	if window == nil || window.UsedPercent == nil || strings.TrimSpace(window.EndTime) == "" {
		return Metric{}, fmt.Errorf("Fireworks Nexus %s usage is missing required fields", name)
	}
	reset, err := time.Parse(time.RFC3339, strings.TrimSpace(window.EndTime))
	if err != nil {
		return Metric{}, fmt.Errorf("parse Fireworks Nexus %s reset time: %w", name, err)
	}
	used, resetAt := clampPercent(*window.UsedPercent), reset.Unix()
	return Metric{Kind: Percentage, Slot: slot, Label: label, Used: &used, ResetAt: &resetAt}, nil
}

// fireworksSeatTier reports whether the tier has included-usage windows.
func fireworksSeatTier(tier string) bool {
	switch strings.ToUpper(strings.TrimSpace(tier)) {
	case "PRO", "TEAMS", "TEAM":
		return true
	default:
		return false
	}
}

func fireworksTierName(tier string) string {
	switch strings.ToUpper(strings.TrimSpace(tier)) {
	case "PRO":
		return "Pro"
	case "TEAMS", "TEAM":
		return "Team"
	case "ENTERPRISE":
		return "Enterprise"
	default:
		tier = strings.TrimSpace(tier)
		if tier == "" {
			return "Nexus"
		}
		return strings.ToUpper(tier[:1]) + strings.ToLower(tier[1:])
	}
}

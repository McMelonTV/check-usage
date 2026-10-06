package providers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/McMelonTV/check-usage/internal/claudeapi"
)

// ClaudeResult is the usage for a Claude account plus its possibly refreshed credentials.
type ClaudeResult struct {
	Usage              Usage
	Credentials        claudeapi.Credentials
	CredentialsChanged bool
	// PlanChecked reports that the profile was read, so Usage.Plan is current.
	PlanChecked bool
}

// ClaudePlanCheckInterval is how often the plan is re-read from the profile
// to notice upgrades or downgrades without spending the usage rate limit.
const ClaudePlanCheckInterval = time.Hour

// ClaudePlanDue reports whether the plan should be re-read.
func ClaudePlanDue(plan string, lastChecked, now time.Time) bool {
	return plan == "" || now.Sub(lastChecked) >= ClaudePlanCheckInterval
}

// FetchClaudeUsage refreshes credentials when needed, then reads the session
// and weekly windows. The plan comes from the profile, which is requested only
// when fetchPlan is set because every request counts against the rate limit.
func FetchClaudeUsage(ctx context.Context, client *http.Client, credentials claudeapi.Credentials, now time.Time, fetchPlan bool) (ClaudeResult, error) {
	credentials, changed, err := claudeapi.RefreshCredentials(ctx, client, credentials, now)
	if err != nil {
		return ClaudeResult{}, err
	}
	result := ClaudeResult{Credentials: credentials, CredentialsChanged: changed}
	var profile *claudeapi.Profile
	profileDone := make(chan struct{})
	go func() {
		defer close(profileDone)
		if fetchPlan {
			profile, _ = claudeapi.FetchProfile(ctx, client, credentials.AccessToken)
		}
	}()
	payload, err := claudeapi.FetchUsage(ctx, client, credentials.AccessToken)
	<-profileDone
	var httpErr *claudeapi.HTTPError
	if err != nil && !changed && errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusUnauthorized {
		// The token was rejected before its recorded expiry; refresh once and retry.
		credentials.ExpiresAt = 0
		credentials, changed, err = claudeapi.RefreshCredentials(ctx, client, credentials, now)
		if err != nil {
			return result, err
		}
		result.Credentials, result.CredentialsChanged = credentials, changed
		payload, err = claudeapi.FetchUsage(ctx, client, credentials.AccessToken)
	}
	if err != nil {
		return result, err
	}
	result.Usage = ClaudeUsage(payload)
	if profile != nil {
		result.Usage.Plan, result.PlanChecked = profile.PlanName(), true
	}
	return result, nil
}

// ClaudeUsage maps the Claude usage payload to session and weekly metrics.
// A per-model weekly limit (e.g. Fable), when present, is placed in the
// otherwise unused monthly slot and marked with Scope.
func ClaudeUsage(payload *claudeapi.UsagePayload) Usage {
	usage := Usage{Metrics: []Metric{
		claudeMetric(SessionSlot, "SESSION", payload.Session()),
		claudeMetric(WeeklySlot, "WEEKLY", payload.Weekly()),
	}}
	if model, window := payload.ModelWeekly(); window != nil {
		metric := claudeMetric(MonthlySlot, strings.ToUpper(model), window)
		metric.Scope = model + " weekly"
		usage.Metrics = append(usage.Metrics, metric)
	}
	return usage
}

func claudeMetric(slot MetricSlot, label string, window *claudeapi.UsageWindow) Metric {
	metric := Metric{Kind: Percentage, Slot: slot, Label: label}
	if window == nil || window.Utilization == nil {
		return metric
	}
	used := clampPercent(*window.Utilization)
	metric.Used = &used
	if resetAt, ok := window.ResetAt(); ok {
		metric.ResetAt = &resetAt
	}
	return metric
}

package providers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/McMelonTV/check-usage/internal/providers/claudeapi"
	"github.com/McMelonTV/check-usage/internal/providers/codexapi"
)

// ClaudeResult is the usage for a Claude account plus its possibly refreshed credentials.
type ClaudeResult struct {
	Usage              Usage
	Credentials        claudeapi.Credentials
	CredentialsChanged bool
	// PlanChecked reports that the profile was read, so Usage.Plan is current.
	PlanChecked bool
	// ResetCredits are the limit resets read with the usage. It is nil when
	// ResetError is set; an account outside the program has an empty list.
	ResetCredits *codexapi.ResetCreditsPayload
	ResetError   error
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
	result.ResetCredits, result.ResetError = ClaudeResetCredits(payload, now)
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

// claudeResetTitle names grants the API sent without a label.
const claudeResetTitle = "Limit reset"

// ClaudeResetCredits maps the limit-reset block to the reset-credit payload
// shared with Codex: one entry per reset, "available" for resets that can be
// used, "redeemed" for used ones, and "expired" or "paused" for the rest of
// a grant that cannot be used. An account outside the program, or one the API
// reports ineligible, holds no credits. Only a malformed block is an error.
func ClaudeResetCredits(payload *claudeapi.UsagePayload, now time.Time) (*codexapi.ResetCreditsPayload, error) {
	status, err := payload.ResetStatus()
	if err != nil {
		return nil, err
	}
	credits := &codexapi.ResetCreditsPayload{Credits: []codexapi.ResetCreditDetail{}}
	if status == nil || !status.Eligible {
		return credits, nil
	}
	credits.AvailableCount = status.AvailableResets(now)
	for _, grant := range status.Grants {
		credits.TotalEarnedCount += grant.ResetsTotal
		detail := codexapi.ResetCreditDetail{Title: grant.Label, GrantedAt: grant.StartsAt, ExpiresAt: grant.EndsAt}
		if detail.Title == "" {
			detail.Title = claudeResetTitle
		}
		unused := "available"
		if !grant.Holding(now) {
			unused = "paused"
			if endsAt, ok := grant.EndTime(); ok && !endsAt.After(now) {
				unused = "expired"
			}
		}
		for range grant.ResetsLeft {
			detail.Status = unused
			credits.Credits = append(credits.Credits, detail)
		}
		for range grant.ResetsTotal - grant.ResetsLeft {
			detail.Status = "redeemed"
			credits.Credits = append(credits.Credits, detail)
		}
	}
	return credits, nil
}

// ClaudeSnapshot builds the provider-neutral snapshot for a Claude account:
// its session and weekly windows plus the reset-credit summary.
func ClaudeSnapshot(usage Usage, credits *codexapi.ResetCreditsPayload, creditsErr error, fetchedAt time.Time) *codexapi.UsageSnapshot {
	snapshot := &codexapi.UsageSnapshot{PlanType: usage.Plan, Windows: []codexapi.UsageWindow{}, FetchedAtMillis: fetchedAt.UnixMilli()}
	for _, metric := range usage.Metrics {
		if metric.IsScoped() || metric.Used == nil {
			continue
		}
		var kind, label string
		var length time.Duration
		switch metric.Slot {
		case SessionSlot:
			kind, label, length = "short_window", "5H", 5*time.Hour
		case WeeklySlot:
			kind, label, length = "long_window", "7D", 7*24*time.Hour
		default:
			continue
		}
		used, remaining, seconds := *metric.Used, 100-*metric.Used, int(length.Seconds())
		snapshot.Windows = append(snapshot.Windows, codexapi.UsageWindow{
			Kind: kind, Label: label, UsedPercent: &used, Remaining: &remaining, ResetAt: metric.ResetAt, WindowSeconds: &seconds,
		})
	}
	snapshot.Credits, snapshot.CreditsError = codexapi.SummarizeResetCredits(credits, creditsErr)
	return snapshot
}

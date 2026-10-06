package usageapi

import (
	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/internal/providers/codexapi"
)

// MetricKind says whether a Metric carries a percentage or free-form text.
type MetricKind string

// MetricSlot is the usage column a Metric is shown in.
type MetricSlot string

const (
	Percentage MetricKind = "percentage"
	Text       MetricKind = "text"

	SessionSlot MetricSlot = "session"
	WeeklySlot  MetricSlot = "weekly"
	MonthlySlot MetricSlot = "monthly"
)

// Metric is one provider usage limit.
type Metric struct {
	Kind MetricKind `json:"kind"`
	Slot MetricSlot `json:"slot"`
	// Label is the short display name of the limit.
	Label string `json:"label"`
	// Used is the used percentage for Percentage metrics.
	Used *float64 `json:"used_percent,omitempty"`
	// ResetAt is when the limit resets, in unix seconds.
	ResetAt *int64 `json:"reset_at,omitempty"`
	// Text is the value of Text metrics.
	Text string `json:"text,omitempty"`
	// Scope names a provider-specific limit, such as Claude's Fable weekly
	// limit or Cursor's model pools. A scoped metric borrows its slot for
	// display only: it is not that slot's session, weekly, or monthly window.
	Scope string `json:"scope,omitempty"`
}

// IsScoped reports whether the metric is a provider-specific limit placed in a borrowed slot.
func (metric Metric) IsScoped() bool {
	return metric.Scope != ""
}

// UsageSnapshot is the detailed Codex usage summary.
type UsageSnapshot struct {
	PlanType        string        `json:"plan_type,omitempty"`
	Windows         []UsageWindow `json:"windows"`
	Credits         *CreditMetric `json:"credits,omitempty"`
	FetchedAtMillis int64         `json:"fetched_at_epoch_millis"`
	CreditsError    string        `json:"credits_error,omitempty"`
}

// UsageWindow is one Codex rate-limit window.
type UsageWindow struct {
	Kind          string   `json:"kind"`
	Label         string   `json:"label"`
	UsedPercent   *float64 `json:"used_percent,omitempty"`
	Remaining     *float64 `json:"remaining_percent,omitempty"`
	ResetAt       *int64   `json:"reset_at,omitempty"`
	WindowSeconds *int     `json:"window_seconds,omitempty"`
}

// CreditMetric summarizes Codex reset credits within a UsageSnapshot.
type CreditMetric struct {
	AvailableCount      int    `json:"available_count"`
	TotalEarnedCount    int    `json:"total_earned_count"`
	EarliestExpiryEpoch *int64 `json:"earliest_expiry_epoch_seconds,omitempty"`
}

// ResetCredits lists the available Codex reset credits of an account.
type ResetCredits struct {
	AvailableCount   int           `json:"available_count"`
	TotalEarnedCount int           `json:"total_earned_count"`
	Credits          []ResetCredit `json:"credits"`
}

// ResetCredit is one Codex reset credit. Times are RFC 3339 strings as
// returned by the provider and may be empty.
type ResetCredit struct {
	Status          string `json:"status"`
	Title           string `json:"title"`
	GrantedAt       string `json:"granted_at"`
	ExpiresAt       string `json:"expires_at"`
	RedeemStartedAt string `json:"redeem_started_at"`
	RedeemedAt      string `json:"redeemed_at"`
}

func publicMetrics(metrics []providers.Metric) []Metric {
	if metrics == nil {
		return nil
	}
	result := make([]Metric, len(metrics))
	for i, metric := range metrics {
		result[i] = Metric{
			Kind: MetricKind(metric.Kind), Slot: MetricSlot(metric.Slot), Label: metric.Label,
			Used: metric.Used, ResetAt: metric.ResetAt, Text: metric.Text, Scope: metric.Scope,
		}
	}
	return result
}

func publicSnapshot(snapshot *codexapi.UsageSnapshot) *UsageSnapshot {
	if snapshot == nil {
		return nil
	}
	result := &UsageSnapshot{
		PlanType: snapshot.PlanType, FetchedAtMillis: snapshot.FetchedAtMillis, CreditsError: snapshot.CreditsError,
	}
	if snapshot.Windows != nil {
		result.Windows = make([]UsageWindow, len(snapshot.Windows))
		for i, window := range snapshot.Windows {
			result.Windows[i] = UsageWindow(window)
		}
	}
	if snapshot.Credits != nil {
		credits := CreditMetric(*snapshot.Credits)
		result.Credits = &credits
	}
	return result
}

// publicResetCredits converts a payload, keeping only available credits.
func publicResetCredits(payload *codexapi.ResetCreditsPayload) *ResetCredits {
	result := &ResetCredits{
		AvailableCount: payload.AvailableCount, TotalEarnedCount: payload.TotalEarnedCount,
		Credits: make([]ResetCredit, 0, len(payload.Credits)),
	}
	for _, credit := range payload.Credits {
		if creditAvailable(credit) {
			result.Credits = append(result.Credits, ResetCredit(credit))
		}
	}
	return result
}

package providers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/McMelonTV/check-usage/internal/providers/cursorapi"
)

// FetchCursorUsage reads the same quota RPCs as the CLI /usage command. The
// returned bearer can be cached locally to avoid exchanging the key each time.
func FetchCursorUsage(ctx context.Context, client *http.Client, key, current string, now time.Time) (Usage, string, error) {
	token, err := cursorapi.AccessToken(ctx, client, key, current, now)
	if err != nil {
		return Usage{}, current, err
	}
	data, err := cursorapi.FetchUsage(ctx, client, token)
	// A bearer may be revoked before expiry. Retry once with a fresh exchange.
	if cursorapi.IsAuthenticationError(err) && token == current {
		token, err = cursorapi.ExchangeAPIKey(ctx, client, key)
		if err == nil {
			data, err = cursorapi.FetchUsage(ctx, client, token)
		}
	}
	if err != nil {
		return Usage{}, current, err
	}
	return cursorUsage(data), token, nil
}

func cursorUsage(data cursorapi.UsageData) Usage {
	usage := Usage{}
	reset := data.Current.BillingCycleEnd
	if data.PlanInfo != nil {
		usage.Plan = data.PlanInfo.PlanName
		if reset <= 0 {
			reset = data.PlanInfo.BillingCycleEnd
		}
	}
	var resetAt *int64
	if reset > 0 {
		value := int64(reset) / 1000
		resetAt = &value
	}
	plan := data.Current.PlanUsage
	// Cursor's pools both run on the monthly billing cycle and have no session
	// window. Like Claude's Fable limit, they borrow the weekly and monthly
	// columns as scoped metrics.
	for _, pool := range []struct {
		slot    MetricSlot
		scope   string
		percent *float64
	}{
		{WeeklySlot, CursorModelsScope, plan.AutoPercentUsed},
		{MonthlySlot, OtherModelsScope, plan.APIPercentUsed},
	} {
		metric := Metric{Kind: Percentage, Slot: pool.slot, Label: strings.ToUpper(pool.scope), Scope: pool.scope, ResetAt: resetAt}
		if pool.percent != nil {
			value := clampPercent(*pool.percent)
			metric.Used = &value
		}
		usage.Metrics = append(usage.Metrics, metric)
	}
	return usage
}

// Cursor metric scopes, shown in place of the slot names.
const (
	CursorModelsScope = "Cursor models"
	OtherModelsScope  = "Other models"
)

// CursorMetrics returns Cursor's placeholder metrics before usage is loaded.
func CursorMetrics() []Metric {
	return []Metric{
		{Kind: Percentage, Slot: WeeklySlot, Label: strings.ToUpper(CursorModelsScope), Scope: CursorModelsScope},
		{Kind: Percentage, Slot: MonthlySlot, Label: strings.ToUpper(OtherModelsScope), Scope: OtherModelsScope},
	}
}

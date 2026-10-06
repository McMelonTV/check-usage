package providers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/McMelonTV/check-usage/cursorapi"
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
	for _, pool := range []struct {
		slot    MetricSlot
		label   string
		percent *float64
	}{
		{CursorModelsSlot, "CURSOR MODELS", plan.AutoPercentUsed},
		{OtherModelsSlot, "OTHER MODELS", plan.APIPercentUsed},
	} {
		metric := Metric{Kind: Percentage, Slot: pool.slot, Label: pool.label, ResetAt: resetAt}
		if pool.percent != nil {
			value := clampPercent(*pool.percent)
			metric.Used = &value
		}
		usage.Metrics = append(usage.Metrics, metric)
	}
	usage.Metrics = append(usage.Metrics, Metric{Kind: Text, Slot: OnDemandSlot, Label: "ON-DEMAND", Text: cursorSpendText(data)})
	return usage
}
func cursorSpendText(data cursorapi.UsageData) string {
	spend, policy := data.Current.SpendLimitUsage, data.HardLimit
	if policy != nil && (policy.NoUsageBasedAllowed || policy.DisabledByOrganization) {
		return "Disabled"
	}
	used := int64(0)
	if spend != nil {
		used = spend.IndividualUsed
		if used < 0 {
			return ""
		}
		if spend.IndividualLimit != nil {
			if *spend.IndividualLimit <= 0 {
				return "Disabled"
			}
			return fmt.Sprintf("USD %.2f / %.2f", float64(used)/100, float64(*spend.IndividualLimit)/100)
		}
		if spend.LimitType == "team" {
			if policy != nil && policy.HardLimit <= 0 {
				return "Disabled"
			}
			if policy != nil || (spend.PooledLimit != nil && *spend.PooledLimit > 0) {
				return fmt.Sprintf("USD %.2f spent (team)", float64(used)/100)
			}
			return ""
		}
	}
	if policy == nil {
		return ""
	}
	if policy.HardLimit <= 0 {
		return "Disabled"
	}
	if policy.HardLimit >= 2147483647 {
		return fmt.Sprintf("USD %.2f spent (unlimited)", float64(used)/100)
	}
	return fmt.Sprintf("USD %.2f / %.2f", float64(used)/100, float64(policy.HardLimit))
}

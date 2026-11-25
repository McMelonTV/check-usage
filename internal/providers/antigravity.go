package providers

import (
	"github.com/McMelonTV/check-usage/internal/providers/antigravityapi"
	"strings"
)

func AntigravityUsage(quotas []antigravityapi.Quota) Usage {
	usage := Usage{}
	for _, quota := range quotas {
		scope := quota.Group
		switch strings.ToLower(scope) {
		case "gemini models":
			scope = "Gemini"
		case "claude and gpt models", "claude/gpt models":
			scope = "Claude/GPT"
		}
		usage.Metrics = append(usage.Metrics, Metric{Kind: Percentage, Slot: MetricSlot(quota.Window), Label: strings.ToUpper(scope + " " + quota.Window), Scope: scope, Used: quota.Used, ResetAt: quota.ResetAt})
	}
	return usage
}

func AntigravityMetrics() []Metric {
	var metrics []Metric
	for _, group := range []string{"Gemini", "Claude/GPT"} {
		for _, slot := range []MetricSlot{SessionSlot, WeeklySlot} {
			metrics = append(metrics, Metric{Kind: Percentage, Slot: slot, Label: strings.ToUpper(group + " " + string(slot)), Scope: group})
		}
	}
	return metrics
}

package remote

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/internal/providers/codexapi"
	"github.com/McMelonTV/check-usage/internal/storage"
	"github.com/McMelonTV/check-usage/internal/usage"
	"github.com/McMelonTV/check-usage/usageapi"
)

func LocalAccount(account usageapi.Account) storage.Account {
	return storage.Account{ID: account.ID, Name: account.Name, Provider: account.Provider, Email: storage.OptionalString(account.Email), PlanType: storage.OptionalString(account.PlanType), AuthData: storage.AuthData{Type: "server"}}
}
func LocalCredits(credits *usageapi.ResetCredits) *codexapi.ResetCreditsPayload {
	if credits == nil {
		return &codexapi.ResetCreditsPayload{Credits: []codexapi.ResetCreditDetail{}}
	}
	payload := &codexapi.ResetCreditsPayload{AvailableCount: credits.AvailableCount, TotalEarnedUnknown: credits.TotalEarnedCount == nil, Credits: []codexapi.ResetCreditDetail{}}
	if credits.TotalEarnedCount != nil {
		payload.TotalEarnedCount = *credits.TotalEarnedCount
	}
	for _, credit := range credits.Credits {
		payload.Credits = append(payload.Credits, codexapi.ResetCreditDetail{Status: credit.Status, Title: credit.Title, GrantedAt: credit.GrantedAt, ExpiresAt: credit.ExpiresAt, RedeemStartedAt: credit.RedeemStartedAt, RedeemedAt: credit.RedeemedAt})
	}
	return payload
}
func Rows(results []usageapi.UsageResult) ([]usage.Row, []storage.Account) {
	rows := make([]usage.Row, 0, len(results))
	accounts := make([]storage.Account, 0, len(results))
	for _, result := range results {
		account := LocalAccount(result.Account)
		accounts = append(accounts, account)
		row := usage.BaseRow(account)
		metrics := make([]providers.Metric, 0, len(result.Metrics))
		for _, metric := range result.Metrics {
			metrics = append(metrics, providers.Metric{Kind: providers.MetricKind(metric.Kind), Slot: providers.MetricSlot(metric.Slot), Label: metric.Label, Used: metric.Used, ResetAt: metric.ResetAt, Scope: metric.Scope, Text: metric.Text})
		}
		usage.ApplyProviderUsage(&row, providers.Usage{Plan: result.Account.PlanType, Metrics: metrics})
		row.Stale = result.Error != "" || result.FetchedAtMillis > 0 && time.Now().UnixMilli()-result.FetchedAtMillis > 30*60*1000
		if result.Error != "" && len(metrics) == 0 {
			row = usage.CachedOrUnavailableRow(account, storage.CacheEntry{}, time.Now())
		}
		if result.Snapshot != nil && result.Snapshot.Credits != nil {
			row.ResetCredits = stringCount(result.Snapshot.Credits.AvailableCount)
			row.ResetCreditsExpireAt = result.Snapshot.Credits.EarliestExpiryEpoch
		}
		rows = append(rows, row)
	}
	return rows, accounts
}
func stringCount(value int) string { return fmt.Sprint(value) }

type DisplayCache struct {
	ServerID string                 `json:"server_id"`
	Results  []usageapi.UsageResult `json:"results"`
}

func SaveDisplayCache(path, serverID string, results []usageapi.UsageResult) error {
	data, err := json.Marshal(DisplayCache{ServerID: serverID, Results: results})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
func LoadDisplayCache(path, serverID string) []usageapi.UsageResult {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cache DisplayCache
	if json.Unmarshal(data, &cache) != nil || cache.ServerID != serverID {
		return nil
	}
	return cache.Results
}

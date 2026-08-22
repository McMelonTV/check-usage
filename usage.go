package main

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

func collectUsageRows(accountsPath string, client *http.Client) ([]usageRow, error) {
	store, err := loadAccountsOrEmpty(accountsPath)
	if err != nil {
		return nil, err
	}
	cache, err := loadUsageCache(store.Accounts)
	if err != nil {
		return nil, err
	}

	rows := make([]usageRow, len(store.Accounts))
	results := make(chan accountResult, len(store.Accounts))

	var wg sync.WaitGroup
	for i := range store.Accounts {
		wg.Add(1)
		acc := store.Accounts[i]
		previousCache := cache[acc.ID]
		go func(idx int, account storedAccount, previous usageCacheEntry) {
			defer wg.Done()

			row := baseUsageRow(account)

			updated := account
			tokenRefreshed := false
			var cache *usageCacheEntry
			result, fetchErr := fetchProviderUsage(context.Background(), client, account)
			if fetchErr != nil {
				if providerCredentialError(fetchErr) || authenticationRequired(fetchErr) {
					row = authenticationRequiredUsageRow(account)
				} else {
					row = cachedOrUnavailableUsageRow(account, previous, time.Now())
				}
				results <- accountResult{Index: idx, Row: row, Updated: updated}
				return
			}
			updated, tokenRefreshed = result.Account, result.AccountChanged
			applyProviderUsage(&row, result.Usage)
			now := time.Now()
			cache = &usageCacheEntry{PlanType: row.Plan, ProviderUsage: &result.Usage, RateLimit: result.RateLimit, FetchedAt: now.Unix()}
			if result.ResetCredits != nil {
				cache.ResetCredits, cache.ResetFetchedAt = result.ResetCredits, now.Unix()
				row.ResetCredits = resetCreditsSummary(result.ResetCredits, now)
			} else if row.SupportsResetCredits {
				if previous.ResetCredits != nil {
					row.ResetCredits = resetCreditsSummary(previous.ResetCredits, now)
					row.ResetsStale = true
				} else if result.ResetError != nil {
					row.ResetCredits = "unavailable"
				}
			}

			results <- accountResult{Index: idx, Row: row, Updated: updated, TokenRefreshed: tokenRefreshed, Cache: cache}
		}(i, acc, previousCache)
	}

	wg.Wait()
	close(results)

	accountsChanged := false
	for result := range results {
		rows[result.Index] = result.Row
		if result.TokenRefreshed {
			store.Accounts[result.Index] = result.Updated
			accountsChanged = true
		}
		if result.Cache != nil {
			if err := mergeAccountUsageCache(result.Updated.ID, *result.Cache); err != nil {
				return nil, err
			}
		}
	}

	if accountsChanged {
		if err := saveAccounts(accountsPath, store); err != nil {
			return nil, err
		}
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].SortName < rows[j].SortName })
	return rows, nil
}

func baseUsageRow(account storedAccount) usageRow {
	provider, _ := providerFor(account.Provider)
	return usageRow{
		ID:                   account.ID,
		Name:                 account.Name,
		ProviderID:           account.Provider,
		Provider:             providerName(account.Provider),
		Email:                valueOrDash(account.Email),
		Plan:                 accountPlan(account),
		Metrics:              emptyProviderMetrics(account.Provider),
		ResetCredits:         "-",
		SupportsResetCredits: provider.ResetCredits,
		SortName:             strings.ToLower(account.Name),
	}
}

func cachedUsageRows(accounts []storedAccount, cache map[string]usageCacheEntry, now time.Time) ([]usageRow, time.Time) {
	rows := make([]usageRow, 0, len(accounts))
	var newest time.Time
	for _, account := range accounts {
		row := baseUsageRow(account)
		entry, ok := cache[account.ID]
		if !ok || entry.FetchedAt <= 0 {
			row.Loading = true
		} else if entry.ProviderUsage != nil {
			applyProviderUsage(&row, *entry.ProviderUsage)
			if row.SupportsResetCredits {
				if entry.ResetCredits != nil {
					row.ResetCredits = resetCreditsSummary(entry.ResetCredits, now)
				} else {
					row.ResetCredits = "unavailable"
				}
			}
			fetchedAt := time.Unix(entry.FetchedAt, 0)
			if fetchedAt.After(newest) {
				newest = fetchedAt
			}
		} else {
			row.Loading = true
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].SortName < rows[j].SortName })
	return rows, newest
}

func applyProviderUsage(row *usageRow, usage providerUsage) {
	row.Plan = firstNonEmpty(usage.Plan, row.Plan)
	row.Metrics = append([]providerMetric(nil), usage.Metrics...)
}

func usageMetricForSlot(row usageRow, slot metricSlot) (providerMetric, bool) {
	for _, metric := range row.Metrics {
		if metric.Slot == slot {
			return metric, true
		}
	}
	return providerMetric{}, false
}

func providerSupportsUsageSlot(providerID string, slot metricSlot) bool {
	switch providerID {
	case providerCodex:
		return slot == sessionSlot || slot == weeklySlot
	case providerOpenCodeGo:
		return slot == sessionSlot || slot == weeklySlot || slot == monthlySlot
	default:
		return false
	}
}

func usageSlotText(row usageRow, slot metricSlot, now time.Time) string {
	if metric, ok := usageMetricForSlot(row, slot); ok {
		return metricText(metric, now)
	}
	if row.AuthRequired && slot == sessionSlot {
		return credentialRequiredText(row)
	}
	if row.Loading && providerSupportsUsageSlot(row.ProviderID, slot) {
		return "loading…"
	}
	return "-"
}

func resetSlotText(row usageRow) string {
	if !row.SupportsResetCredits {
		return "-"
	}
	if row.Loading && row.ResetCredits == "-" {
		return "loading…"
	}
	return firstNonEmpty(row.ResetCredits, "unavailable")
}

func cachedOrUnavailableUsageRow(account storedAccount, entry usageCacheEntry, now time.Time) usageRow {
	if entry.FetchedAt > 0 {
		rows, _ := cachedUsageRows([]storedAccount{account}, map[string]usageCacheEntry{account.ID: entry}, now)
		row := rows[0]
		row.Stale = true
		return row
	}
	row := baseUsageRow(account)
	row.Metrics = nil
	if row.SupportsResetCredits {
		row.ResetCredits = "unavailable"
	}
	return row
}

func authenticationRequiredUsageRow(account storedAccount) usageRow {
	row := baseUsageRow(account)
	row.Metrics = nil
	if row.SupportsResetCredits {
		row.ResetCredits = "sign in required"
	}
	row.AuthRequired = true
	return row
}

func slotRank(slot metricSlot) (int, bool) {
	switch slot {
	case sessionSlot:
		return 0, true
	case weeklySlot:
		return 1, true
	case monthlySlot:
		return 2, true
	default:
		return 0, false
	}
}

func isSlotBlockedByLongerWindow(row usageRow, slot metricSlot) bool {
	rank, ok := slotRank(slot)
	if !ok {
		return false
	}
	for _, metric := range row.Metrics {
		otherRank, ok := slotRank(metric.Slot)
		if !ok || otherRank <= rank {
			continue
		}
		if metric.Used == nil {
			continue
		}
		if percentValue(*metric.Used) >= 100 {
			return true
		}
	}
	return false
}

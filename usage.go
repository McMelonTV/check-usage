package main

import (
	"context"
	"errors"
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
				if errors.Is(fetchErr, errMissingAPIKey) || authenticationRequired(fetchErr) {
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
			cache = &usageCacheEntry{PlanType: row.Plan, ProviderUsage: &result.Usage, FetchedAt: now.Unix()}
			if result.ResetCredits != nil {
				cache.ResetCredits, cache.ResetFetchedAt = result.ResetCredits, now.Unix()
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
	return usageRow{
		ID:             account.ID,
		Name:           account.Name,
		Provider:       providerName(account.Provider),
		Email:          valueOrDash(account.Email),
		Plan:           valueOrDash(account.PlanType),
		Primary:        "-",
		Secondary:      "-",
		ResetCredits:   "-",
		PrimaryLabel:   "PRIMARY",
		SecondaryLabel: "SECONDARY",
		DetailsLabel:   "DETAILS",
		SortName:       strings.ToLower(account.Name),
	}
}

func cachedUsageRows(accounts []storedAccount, cache map[string]usageCacheEntry, now time.Time) ([]usageRow, time.Time) {
	rows := make([]usageRow, 0, len(accounts))
	var newest time.Time
	for _, account := range accounts {
		row := baseUsageRow(account)
		entry, ok := cache[account.ID]
		if !ok || entry.FetchedAt <= 0 {
			row.Primary, row.Secondary, row.ResetCredits = "loading…", "loading…", "loading…"
			row.Loading = true
		} else {
			if entry.ProviderUsage != nil {
				applyProviderUsage(&row, *entry.ProviderUsage)
			} else {
				applyLegacyCodexCache(&row, entry, now)
			}
			fetchedAt := time.Unix(entry.FetchedAt, 0)
			if fetchedAt.After(newest) {
				newest = fetchedAt
			}
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].SortName < rows[j].SortName })
	return rows, newest
}

func applyLegacyCodexCache(row *usageRow, entry usageCacheEntry, now time.Time) {
	usage := providerUsage{
		Plan:      entry.PlanType,
		Primary:   providerMetric{Label: "5H", Summary: limitSummary(entry.RateLimit, true, now), Used: windowUsedPercent(entry.RateLimit, true)},
		Secondary: providerMetric{Label: "WEEK", Summary: limitSummary(entry.RateLimit, false, now), Used: windowUsedPercent(entry.RateLimit, false)},
		Details:   providerMetric{Label: "RESETS", Summary: "unavailable"},
	}
	if entry.ResetCredits != nil {
		usage.Details.Summary = resetCreditsSummary(entry.ResetCredits, now)
	}
	applyProviderUsage(row, usage)
}

func applyProviderUsage(row *usageRow, usage providerUsage) {
	row.Plan = firstNonEmpty(usage.Plan, row.Plan)
	row.Primary, row.PrimaryLabel, row.PrimaryUsed = firstNonEmpty(usage.Primary.Summary, "n/a"), firstNonEmpty(usage.Primary.Label, "PRIMARY"), usage.Primary.Used
	row.Secondary, row.SecondaryLabel, row.SecondaryUsed = firstNonEmpty(usage.Secondary.Summary, "n/a"), firstNonEmpty(usage.Secondary.Label, "SECONDARY"), usage.Secondary.Used
	row.ResetCredits, row.DetailsLabel = firstNonEmpty(usage.Details.Summary, "n/a"), firstNonEmpty(usage.Details.Label, "DETAILS")
}

func cachedOrUnavailableUsageRow(account storedAccount, entry usageCacheEntry, now time.Time) usageRow {
	if entry.FetchedAt > 0 {
		rows, _ := cachedUsageRows([]storedAccount{account}, map[string]usageCacheEntry{account.ID: entry}, now)
		row := rows[0]
		row.Stale = true
		return row
	}
	row := baseUsageRow(account)
	row.Primary, row.Secondary, row.ResetCredits = "n/a", "n/a", "n/a"
	return row
}

func authenticationRequiredUsageRow(account storedAccount) usageRow {
	row := baseUsageRow(account)
	row.Primary, row.Secondary, row.ResetCredits = "sign in required", "sign in required", "sign in required"
	row.AuthRequired = true
	return row
}

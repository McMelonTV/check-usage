package main

import (
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
			switch normalizeAuthType(account.AuthData.Type) {
			case "apikey":
				row.Primary = "n/a"
				row.Secondary = "n/a"
				row.ResetCredits = "n/a"
			case "chatgpt":
				refreshedAcc, changed, refreshErr := ensureFreshTokens(account, client)
				if refreshErr != nil {
					if authenticationRequired(refreshErr) {
						row = authenticationRequiredUsageRow(account)
					} else {
						row = cachedOrUnavailableUsageRow(account, previous, time.Now())
					}
					results <- accountResult{Index: idx, Row: row, Updated: updated}
					return
				}

				updated = refreshedAcc
				tokenRefreshed = changed

				var usage *rateLimitStatusPayload
				var resetCredits *resetCreditsPayload
				var usageErr, resetCreditsErr error
				var requestWG sync.WaitGroup
				requestWG.Add(2)
				go func() {
					defer requestWG.Done()
					usage, usageErr = fetchUsage(updated, client)
				}()
				go func() {
					defer requestWG.Done()
					resetCredits, resetCreditsErr = fetchResetCredits(updated, client)
				}()
				requestWG.Wait()
				if usageErr != nil {
					if authenticationRequired(usageErr) {
						row = authenticationRequiredUsageRow(account)
					} else {
						row = cachedOrUnavailableUsageRow(account, previous, time.Now())
					}
					results <- accountResult{Index: idx, Row: row, Updated: updated, TokenRefreshed: tokenRefreshed}
					return
				}

				row.Plan = firstNonEmpty(usage.PlanType, row.Plan)
				now := time.Now()
				row.Primary = limitSummary(usage.RateLimit, true, now)
				row.Secondary = limitSummary(usage.RateLimit, false, now)
				row.PrimaryUsed = windowUsedPercent(usage.RateLimit, true)
				row.SecondaryUsed = windowUsedPercent(usage.RateLimit, false)
				entry := usageCacheEntry{
					PlanType:  row.Plan,
					RateLimit: usage.RateLimit,
					FetchedAt: now.Unix(),
				}
				if resetCreditsErr != nil {
					if previous.ResetCredits != nil {
						row.ResetCredits = resetCreditsSummary(previous.ResetCredits, now)
						row.ResetsStale = true
					} else {
						row.ResetCredits = "unavailable"
					}
				} else {
					row.ResetCredits = resetCreditsSummary(resetCredits, now)
					entry.ResetCredits = resetCredits
					entry.ResetFetchedAt = now.Unix()
				}
				cache = &entry
			default:
				row.Primary = "n/a"
				row.Secondary = "n/a"
				row.ResetCredits = "n/a"
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
		ID:           account.ID,
		Name:         account.Name,
		Provider:     account.Provider,
		Email:        valueOrDash(account.Email),
		Plan:         valueOrDash(account.PlanType),
		Primary:      "-",
		Secondary:    "-",
		ResetCredits: "-",
		SortName:     strings.ToLower(account.Name),
	}
}

func cachedUsageRows(accounts []storedAccount, cache map[string]usageCacheEntry, now time.Time) ([]usageRow, time.Time) {
	rows := make([]usageRow, 0, len(accounts))
	var newest time.Time
	for _, account := range accounts {
		row := baseUsageRow(account)
		switch normalizeAuthType(account.AuthData.Type) {
		case "apikey":
			row.Primary, row.Secondary, row.ResetCredits = "n/a", "n/a", "n/a"
		case "chatgpt":
			entry, ok := cache[account.ID]
			if !ok || entry.FetchedAt <= 0 {
				row.Primary, row.Secondary, row.ResetCredits = "loading…", "loading…", "loading…"
				row.Loading = true
				break
			}
			row.Plan = firstNonEmpty(entry.PlanType, row.Plan)
			row.Primary = limitSummary(entry.RateLimit, true, now)
			row.Secondary = limitSummary(entry.RateLimit, false, now)
			row.PrimaryUsed = windowUsedPercent(entry.RateLimit, true)
			row.SecondaryUsed = windowUsedPercent(entry.RateLimit, false)
			if entry.ResetCredits != nil {
				row.ResetCredits = resetCreditsSummary(entry.ResetCredits, now)
			} else {
				row.ResetCredits = "unavailable"
			}
			fetchedAt := time.Unix(entry.FetchedAt, 0)
			if fetchedAt.After(newest) {
				newest = fetchedAt
			}
		default:
			row.Primary, row.Secondary, row.ResetCredits = "n/a", "n/a", "n/a"
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].SortName < rows[j].SortName })
	return rows, newest
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

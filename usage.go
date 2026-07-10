package main

import (
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

func collectUsageRows(accountsPath string, client *http.Client) ([]usageRow, error) {
	store, err := loadAccounts(accountsPath)
	if err != nil {
		return nil, err
	}

	rows := make([]usageRow, len(store.Accounts))
	results := make(chan accountResult, len(store.Accounts))

	var wg sync.WaitGroup
	for i := range store.Accounts {
		wg.Add(1)
		acc := store.Accounts[i]
		go func(idx int, account storedAccount) {
			defer wg.Done()

			row := usageRow{
				Name:         account.Name,
				Email:        valueOrDash(account.Email),
				Plan:         valueOrDash(account.PlanType),
				Primary:      "-",
				Secondary:    "-",
				ResetCredits: "-",
				SortName:     strings.ToLower(account.Name),
			}

			updated := account
			tokenRefreshed := false

			switch normalizeAuthType(account.AuthData.Type) {
			case "apikey":
				row.Primary = "n/a"
				row.Secondary = "n/a"
				row.ResetCredits = "n/a"
			case "chatgpt":
				refreshedAcc, changed, refreshErr := ensureFreshTokens(account, client)
				if refreshErr != nil {
					row.Primary = "n/a"
					row.Secondary = "n/a"
					row.ResetCredits = "n/a"
					results <- accountResult{Index: idx, Row: row, Updated: updated}
					return
				}

				updated = refreshedAcc
				tokenRefreshed = changed

				usage, usageErr := fetchUsage(updated, client)
				if usageErr != nil {
					row.Primary = "n/a"
					row.Secondary = "n/a"
					row.ResetCredits = "n/a"
					results <- accountResult{Index: idx, Row: row, Updated: updated, TokenRefreshed: tokenRefreshed}
					return
				}

				row.Plan = firstNonEmpty(usage.PlanType, row.Plan)
				now := time.Now()
				row.Primary = limitSummary(usage.RateLimit, true, now)
				row.Secondary = limitSummary(usage.RateLimit, false, now)
				row.PrimaryUsed = windowUsedPercent(usage.RateLimit, true)
				row.SecondaryUsed = windowUsedPercent(usage.RateLimit, false)
				resetCredits, resetCreditsErr := fetchResetCredits(updated, client)
				if resetCreditsErr != nil {
					row.ResetCredits = "unavailable"
				} else {
					row.ResetCredits = resetCreditsSummary(resetCredits, now)
				}
			default:
				row.Primary = "n/a"
				row.Secondary = "n/a"
				row.ResetCredits = "n/a"
			}

			results <- accountResult{Index: idx, Row: row, Updated: updated, TokenRefreshed: tokenRefreshed}
		}(i, acc)
	}

	wg.Wait()
	close(results)

	needsSave := false
	for result := range results {
		rows[result.Index] = result.Row
		if result.TokenRefreshed {
			store.Accounts[result.Index] = result.Updated
			needsSave = true
		}
	}

	if needsSave {
		if err := saveAccounts(accountsPath, store); err != nil {
			return nil, err
		}
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].SortName < rows[j].SortName })
	return rows, nil
}

// Package usage fetches provider usage for saved accounts and formats it as dashboard rows and plain tables.
package usage

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/internal/storage"
)

func CollectRows(accountsPath string, client *http.Client) ([]Row, error) {
	store, err := storage.LoadAccountsOrEmpty(accountsPath)
	if err != nil {
		return nil, err
	}
	cache, err := storage.LoadCache(store.Accounts)
	if err != nil {
		return nil, err
	}

	rows := make([]Row, len(store.Accounts))
	results := make(chan accountResult, len(store.Accounts))

	var wg sync.WaitGroup
	for i := range store.Accounts {
		wg.Add(1)
		acc := store.Accounts[i]
		previousCache := cache[acc.ID]
		go func(idx int, account storage.Account, previous storage.CacheEntry) {
			defer wg.Done()

			row := BaseRow(account)

			updated := account
			tokenRefreshed := false
			var cache *storage.CacheEntry
			if throttled, ok := ThrottledRow(account, previous, time.Now()); ok {
				results <- accountResult{Index: idx, Row: throttled, Updated: updated}
				return
			}
			result, fetchErr := FetchProviderUsage(context.Background(), client, account)
			if fetchErr != nil {
				if providerCredentialError(fetchErr) || authenticationRequired(fetchErr) {
					row = AuthenticationRequiredRow(account)
				} else {
					row = CachedOrUnavailableRow(account, previous, time.Now())
				}
				if !result.NextFetchAt.IsZero() {
					// Rate limited: back off, and show the cache as a throttled
					// row instead of a stale one while it is still recent.
					cache = &storage.CacheEntry{NextFetchAt: result.NextFetchAt.Unix()}
					previous.NextFetchAt = cache.NextFetchAt
					if throttled, ok := ThrottledRow(account, previous, time.Now()); ok {
						row = throttled
					}
				}
				if result.AccountChanged {
					updated, tokenRefreshed = result.Account, true
				}
				results <- accountResult{Index: idx, Row: row, Updated: updated, TokenRefreshed: tokenRefreshed, Cache: cache}
				return
			}
			updated, tokenRefreshed = result.Account, result.AccountChanged
			ApplyProviderUsage(&row, result.Usage)
			now := time.Now()
			cache = &storage.CacheEntry{PlanType: row.Plan, ProviderUsage: &result.Usage, RateLimit: result.RateLimit, FetchedAt: now.Unix()}
			if !result.NextFetchAt.IsZero() {
				cache.NextFetchAt = result.NextFetchAt.Unix()
			}
			if !result.PlanCheckedAt.IsZero() {
				cache.PlanCheckedAt = result.PlanCheckedAt.Unix()
			}
			if result.ResetCredits != nil {
				cache.ResetCredits, cache.ResetFetchedAt = result.ResetCredits, now.Unix()
				setResetCredits(&row, result.ResetCredits, now)
			} else if row.SupportsResetCredits {
				if previous.ResetCredits != nil {
					setResetCredits(&row, previous.ResetCredits, now)
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
			if err := storage.MergeAccountCache(result.Updated.ID, *result.Cache); err != nil {
				return nil, err
			}
		}
	}

	if accountsChanged {
		if err := storage.SaveAccounts(accountsPath, store); err != nil {
			return nil, err
		}
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].SortName < rows[j].SortName })
	return rows, nil
}

func BaseRow(account storage.Account) Row {
	provider, _ := ProviderFor(account.Provider)
	return Row{
		ID:                   account.ID,
		Name:                 account.Name,
		ProviderID:           account.Provider,
		Provider:             ProviderName(account.Provider),
		Email:                ValueOrDash(account.Email),
		Plan:                 AccountPlan(account),
		Metrics:              emptyProviderMetrics(account.Provider),
		ResetCredits:         "-",
		SupportsResetCredits: provider.ResetCredits,
		SortName:             strings.ToLower(account.Name),
	}
}

func CachedRows(accounts []storage.Account, cache map[string]storage.CacheEntry, now time.Time) ([]Row, time.Time) {
	rows := make([]Row, 0, len(accounts))
	var newest time.Time
	for _, account := range accounts {
		row := BaseRow(account)
		entry, ok := cache[account.ID]
		if !ok || entry.FetchedAt <= 0 {
			row.Loading = true
		} else if entry.ProviderUsage != nil {
			ApplyProviderUsage(&row, *entry.ProviderUsage)
			if row.SupportsResetCredits {
				if entry.ResetCredits != nil {
					setResetCredits(&row, entry.ResetCredits, now)
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

// throttledStaleAfter is how old throttled cached usage may be before the
// dashboard marks it stale anyway.
const throttledStaleAfter = 30 * time.Minute

// ThrottledRow returns the cached row for an account whose provider must
// not be asked again yet (see storage.CacheEntry.NextFetchAt).
func ThrottledRow(account storage.Account, entry storage.CacheEntry, now time.Time) (Row, bool) {
	if entry.NextFetchAt <= now.Unix() || entry.ProviderUsage == nil || entry.FetchedAt <= 0 {
		return Row{}, false
	}
	rows, _ := CachedRows([]storage.Account{account}, map[string]storage.CacheEntry{account.ID: entry}, now)
	row := rows[0]
	row.Stale = now.Sub(time.Unix(entry.FetchedAt, 0)) > throttledStaleAfter
	return row, true
}

func ApplyProviderUsage(row *Row, usage providers.Usage) {
	row.Plan = FirstNonEmpty(usage.Plan, row.Plan)
	row.Metrics = append([]providers.Metric(nil), usage.Metrics...)
}

// ScopedMarker flags a provider-specific limit shown in a borrowed column.
const ScopedMarker = "✦"

// Slots are the three usage columns shared by every provider.
var Slots = [3]providers.MetricSlot{providers.SessionSlot, providers.WeeklySlot, providers.MonthlySlot}

// ColumnLabels are the headers of the usage columns.
var ColumnLabels = [3]string{"SESSION (~5h)", "WEEKLY", "MONTHLY"}

// SlotLabel is the short label for a slot, naming the scope for scoped metrics.
func SlotLabel(row Row, slot providers.MetricSlot) string {
	if metric, ok := MetricForSlot(row, slot); ok && metric.IsScoped() {
		return strings.ToUpper(metric.Scope)
	}
	return strings.ToUpper(string(slot))
}

func MetricForSlot(row Row, slot providers.MetricSlot) (providers.Metric, bool) {
	for _, metric := range row.Metrics {
		if metric.Slot == slot {
			return metric, true
		}
	}
	return providers.Metric{}, false
}

func providerSupportsUsageSlot(providerID string, slot providers.MetricSlot) bool {
	switch providerID {
	case providers.Codex, providers.Claude:
		return slot == providers.SessionSlot || slot == providers.WeeklySlot
	case providers.OpenCodeGo:
		return slot == providers.SessionSlot || slot == providers.WeeklySlot || slot == providers.MonthlySlot
	case providers.Cursor:
		return slot == providers.WeeklySlot || slot == providers.MonthlySlot
	default:
		return false
	}
}

func SlotText(row Row, slot providers.MetricSlot, now time.Time) string {
	if row.AuthRequired && slot == providers.SessionSlot {
		return credentialRequiredText(row)
	}
	if metric, ok := MetricForSlot(row, slot); ok {
		if row.Loading && metric.Used == nil && metric.Text == "" {
			return "loading…"
		}
		if text := MetricText(metric, now); metric.IsScoped() && text != "-" {
			// The scope name is shown with the reset time below the value.
			return ScopedMarker + " " + text
		}
		return MetricText(metric, now)
	}
	if row.Loading && providerSupportsUsageSlot(row.ProviderID, slot) {
		return "loading…"
	}
	return "-"
}

func ResetSlotText(row Row) string {
	if !row.SupportsResetCredits {
		return "-"
	}
	if row.Loading && row.ResetCredits == "-" {
		return "loading…"
	}
	return FirstNonEmpty(row.ResetCredits, "unavailable")
}

func CachedOrUnavailableRow(account storage.Account, entry storage.CacheEntry, now time.Time) Row {
	if entry.FetchedAt > 0 {
		rows, _ := CachedRows([]storage.Account{account}, map[string]storage.CacheEntry{account.ID: entry}, now)
		row := rows[0]
		row.Stale = true
		return row
	}
	row := BaseRow(account)
	row.Metrics = nil
	if row.SupportsResetCredits {
		row.ResetCredits = "unavailable"
	}
	return row
}

func AuthenticationRequiredRow(account storage.Account) Row {
	row := BaseRow(account)
	row.Metrics = nil
	if row.SupportsResetCredits {
		row.ResetCredits = "sign in required"
	}
	row.AuthRequired = true
	return row
}

func slotRank(slot providers.MetricSlot) (int, bool) {
	switch slot {
	case providers.SessionSlot:
		return 0, true
	case providers.WeeklySlot:
		return 1, true
	case providers.MonthlySlot:
		return 2, true
	default:
		return 0, false
	}
}

func IsSlotBlockedByLongerWindow(row Row, slot providers.MetricSlot) bool {
	rank, ok := slotRank(slot)
	if !ok {
		return false
	}
	if target, ok := MetricForSlot(row, slot); ok && target.IsScoped() {
		// A scoped limit is blocked only by a longer shared limit (the weekly window).
		rank, _ = slotRank(providers.SessionSlot)
	}
	for _, metric := range row.Metrics {
		// Exhausting a scoped limit leaves the other windows usable.
		if metric.IsScoped() {
			continue
		}
		otherRank, ok := slotRank(metric.Slot)
		if !ok || otherRank <= rank {
			continue
		}
		if metric.Used == nil {
			continue
		}
		if PercentValue(*metric.Used) >= 100 {
			return true
		}
	}
	return false
}

package usageapi

import (
	"context"
	"fmt"
	"strings"

	"github.com/McMelonTV/check-usage/internal/nativecli"
	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/internal/providers/codexapi"
)

// NativeAccount is a local administration operation. It is not exposed over HTTP.
type NativeAccount struct {
	Account  string
	Provider string
	Name     string
	Config   nativecli.Config
}

func (service *Service) SaveNativeAccount(request NativeAccount) (AccountMutation, error) {
	if _, ok := providers.Get(request.Provider); !ok {
		return AccountMutation{}, fmt.Errorf("unsupported provider %q", request.Provider)
	}
	if err := request.Config.Validate(request.Provider); err != nil {
		return AccountMutation{}, err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	store, err := service.loadAccounts()
	if err != nil {
		return AccountMutation{}, err
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		definition, _ := providers.Get(request.Provider)
		name = definition.Name
	}
	account := storedAccount{ID: newAccountID(service.now()), Name: name, Provider: request.Provider, AuthData: authData{Type: "native_cli", Native: &request.Config}}
	action := "added"
	if request.Account != "" {
		index, err := findAccount(store.Accounts, request.Account)
		if err != nil {
			return AccountMutation{}, err
		}
		if store.Accounts[index].Provider != request.Provider {
			return AccountMutation{}, fmt.Errorf("provider does not match existing account")
		}
		account = store.Accounts[index]
		account.AuthData.Native = &request.Config
		if request.Name != "" {
			account.Name = name
		}
		store.Accounts[index] = account
		action = "updated"
	} else {
		for _, existing := range store.Accounts {
			if strings.EqualFold(existing.Name, name) {
				return AccountMutation{}, fmt.Errorf("account name already exists")
			}
		}
		store.Accounts = append(store.Accounts, account)
	}
	if err := service.saveAccounts(store); err != nil {
		return AccountMutation{}, err
	}
	if err := service.clearCache(account.ID); err != nil {
		return AccountMutation{}, err
	}
	return AccountMutation{Action: action, Account: account.public()}, nil
}

func (service *Service) nativeUsage(ctx context.Context, account *storedAccount, refresh bool) (UsageResult, bool, error) {
	config := *account.AuthData.Native
	entry, cached, err := service.loadCache(account.ID)
	if err != nil {
		return UsageResult{}, false, err
	}
	fromCache := func() UsageResult {
		return UsageResult{Account: account.public(), Metrics: publicMetrics(entry.ProviderUsage.Metrics), Snapshot: nativeSnapshot(*entry.ProviderUsage, entry.ResetCredits, entry.FetchedAt), Cached: true, Error: entry.NativeError, Source: firstNonEmpty(entry.Source, "native_cli")}
	}
	if !refresh || entry.NextFetchAt > service.now().Unix() {
		if cached && entry.ProviderUsage != nil {
			return fromCache(), false, nil
		}
		return UsageResult{}, false, fmt.Errorf("native usage unavailable; wait for the next refresh or check the CLI on the server")
	}
	fetched, err := service.nativeFetch(ctx, account.Provider, config)
	now := service.now()
	entry.NextFetchAt = now.Add(config.Interval()).Unix()
	if err == nil {
		if fetched.AccountID != "" && stringValue(account.AuthData.AccountID) != "" && fetched.AccountID != stringValue(account.AuthData.AccountID) {
			err = fmt.Errorf("native CLI is signed in to a different account; update its server profile")
		} else if stringValue(account.Email) != "" && fetched.Email != "" && !strings.EqualFold(stringValue(account.Email), fetched.Email) {
			err = fmt.Errorf("native CLI email does not match this account")
		}
	}
	if err != nil {
		entry.NativeError = "Native usage unavailable; check its login or adapter on the server."
		// Only administrator-enabled fallbacks may use existing provider credentials.
		if config.Fallback && account.AuthData.Type != "native_cli" && account.Provider != providers.Antigravity {
			fallback := *account
			fallback.AuthData.Native = nil
			result, changed, fallbackErr := service.usageForAccount(ctx, &fallback, true)
			fallback.AuthData.Native = account.AuthData.Native
			if changed {
				*account = fallback
			}
			updated, _, loadErr := service.loadCache(account.ID)
			if loadErr != nil {
				return UsageResult{}, changed, loadErr
			}
			updated.NextFetchAt = max(updated.NextFetchAt, entry.NextFetchAt)
			if fallbackErr == nil && result.Error == "" && !result.Cached {
				result.Source = "api_fallback"
				updated.Source = "api_fallback"
				updated.NativeError = ""
			} else if fallbackErr != nil || result.Error != "" {
				updated.NativeError = "Native usage and API fallback unavailable; check this account on the server."
			}
			if saveErr := service.saveCache(account.ID, updated); saveErr != nil {
				return UsageResult{}, changed, saveErr
			}
			return result, changed, fallbackErr
		}
		if saveErr := service.saveCache(account.ID, entry); saveErr != nil {
			return UsageResult{}, false, saveErr
		}
		if cached && entry.ProviderUsage != nil {
			result := fromCache()
			result.Error = err.Error()
			return result, false, nil
		}
		return UsageResult{}, false, err
	}
	changed := false
	if fetched.AccountID != "" && fetched.AccountID != stringValue(account.AuthData.AccountID) {
		account.AuthData.AccountID = stringPointer(fetched.AccountID)
		changed = true
	}
	if fetched.Email != "" && fetched.Email != stringValue(account.Email) {
		account.Email = stringPointer(fetched.Email)
		changed = true
	}
	if fetched.Usage.Plan != "" && fetched.Usage.Plan != stringValue(account.PlanType) {
		account.PlanType = stringPointer(fetched.Usage.Plan)
		changed = true
	}
	entry.Source = "native_cli"
	entry.NativeError = ""
	entry.ProviderUsage, entry.FetchedAt, entry.PlanType = &fetched.Usage, now.Unix(), fetched.Usage.Plan
	if fetched.ResetCredits != nil {
		entry.ResetCredits, entry.ResetFetchedAt = fetched.ResetCredits, now.Unix()
	}
	if err := service.saveCache(account.ID, entry); err != nil {
		return UsageResult{}, changed, err
	}
	return UsageResult{Account: account.public(), Metrics: publicMetrics(fetched.Usage.Metrics), Snapshot: nativeSnapshot(fetched.Usage, entry.ResetCredits, entry.FetchedAt), Source: "native_cli"}, changed, nil
}

func nativeSnapshot(usage providers.Usage, credits *codexapi.ResetCreditsPayload, fetchedAt int64) *UsageSnapshot {
	snapshot := &UsageSnapshot{PlanType: usage.Plan, FetchedAtMillis: fetchedAt * 1000, Windows: []UsageWindow{}}
	for _, metric := range usage.Metrics {
		kind := "short_window"
		if metric.Slot == providers.WeeklySlot {
			kind = "long_window"
		} else if metric.Slot == providers.MonthlySlot {
			kind = "monthly_window"
		}
		window := UsageWindow{Kind: kind, Label: metric.Label, UsedPercent: metric.Used, ResetAt: metric.ResetAt}
		if metric.Used != nil {
			left := 100 - *metric.Used
			window.Remaining = &left
		}
		snapshot.Windows = append(snapshot.Windows, window)
	}
	if credits != nil {
		summary, _ := codexapi.SummarizeResetCredits(credits, nil)
		snapshot.Credits = &CreditMetric{AvailableCount: summary.AvailableCount, EarliestExpiryEpoch: summary.EarliestExpiryEpoch}
		if !credits.TotalEarnedUnknown {
			snapshot.Credits.TotalEarnedCount = &credits.TotalEarnedCount
		}
	}
	return snapshot
}

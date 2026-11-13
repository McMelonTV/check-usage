package usage

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/internal/providers/codexapi"
	"github.com/McMelonTV/check-usage/internal/storage"
)

// RefreshResetCredits fetches and caches the reset credits of a reset-capable
// account. It returns the account with any renewed credentials and whether it
// changed, so the caller can persist it.
//
// Codex has a dedicated reset-credit endpoint. Claude reports resets inside its
// rate-limited usage response, so a Claude refresh is a full usage refresh: it
// honors the shared throttle and serves the cached credits within it.
func RefreshResetCredits(account storage.Account, client *http.Client) (storage.Account, bool, *codexapi.ResetCreditsPayload, error) {
	provider, err := ProviderFor(account.Provider)
	if err != nil {
		return account, false, nil, err
	}
	if !provider.ResetCredits {
		return account, false, nil, fmt.Errorf("reset credits are unavailable for %s accounts", provider.Name)
	}
	if provider.ID == providers.Claude {
		return refreshClaudeResetCredits(account, client)
	}
	updated, changed, err := EnsureFreshTokens(account, client)
	if err != nil {
		return account, false, nil, err
	}
	payload, err := FetchResetCredits(updated, client)
	if err != nil {
		return updated, changed, nil, err
	}
	if err := storage.UpdateResetCache(updated.ID, payload, time.Now().Unix()); err != nil {
		return updated, changed, nil, err
	}
	return updated, changed, payload, nil
}

func refreshClaudeResetCredits(account storage.Account, client *http.Client) (storage.Account, bool, *codexapi.ResetCreditsPayload, error) {
	previous, _, err := storage.LoadAccountCache(account.ID)
	if err != nil {
		return account, false, nil, err
	}
	if previous.NextFetchAt > time.Now().Unix() && previous.ResetCredits != nil {
		return account, false, previous.ResetCredits, nil
	}
	result, err := FetchProviderUsage(context.Background(), client, account)
	if err != nil {
		if !result.NextFetchAt.IsZero() {
			if mergeErr := storage.MergeAccountCache(account.ID, storage.CacheEntry{NextFetchAt: result.NextFetchAt.Unix()}); mergeErr != nil {
				return result.Account, result.AccountChanged, nil, mergeErr
			}
			if previous.ResetCredits != nil {
				// Rate limited: the cached credits are the best answer for now.
				return result.Account, result.AccountChanged, previous.ResetCredits, nil
			}
		}
		return result.Account, result.AccountChanged, nil, err
	}
	now := time.Now()
	update := storage.CacheEntry{
		PlanType: FirstNonEmpty(result.Usage.Plan, storage.StringValue(result.Account.PlanType)), ProviderUsage: &result.Usage,
		FetchedAt: now.Unix(), NextFetchAt: result.NextFetchAt.Unix(),
		ResetCredits: result.ResetCredits, ResetFetchedAt: now.Unix(),
	}
	if !result.PlanCheckedAt.IsZero() {
		update.PlanCheckedAt = result.PlanCheckedAt.Unix()
	}
	if err := storage.MergeAccountCache(account.ID, update); err != nil {
		return result.Account, result.AccountChanged, nil, err
	}
	if result.ResetError != nil {
		return result.Account, result.AccountChanged, nil, result.ResetError
	}
	return result.Account, result.AccountChanged, result.ResetCredits, nil
}

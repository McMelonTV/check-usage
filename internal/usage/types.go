package usage

import (
	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/internal/storage"
)

type Row struct {
	ID           string
	Name         string
	ProviderID   string
	Provider     string
	Email        string
	Plan         string
	Metrics      []providers.Metric
	ResetCredits string
	// ResetCreditsExpireAt is when the earliest available reset credit expires (unix seconds).
	ResetCreditsExpireAt *int64
	SupportsResetCredits bool
	SortName             string
	Loading              bool
	AuthRequired         bool
	Stale                bool
	ResetsStale          bool
}

type accountResult struct {
	Index          int
	Row            Row
	Updated        storage.Account
	TokenRefreshed bool
	Cache          *storage.CacheEntry
}

package usageapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/internal/providers/claudeapi"
	"github.com/McMelonTV/check-usage/internal/providers/codexapi"
	"github.com/McMelonTV/check-usage/internal/providers/cursorapi"
)

// Config controls persistence, networking, and time for an embedded Service.
type Config struct {
	AccountsFile string
	CacheDir     string
	HTTPClient   *http.Client
	UserAgent    string
	Now          func() time.Time
}

// Service is the high-level, credential-owning check-usage application API.
// Its methods are safe for concurrent use within one process.
type Service struct {
	accountsFile    string
	cacheDir        string
	client          *http.Client
	userAgent       string
	now             func() time.Time
	mu              sync.Mutex
	browserSessions map[string]cursorapi.LoginSession
}

// New creates a Service, applying platform defaults to omitted configuration.
func New(config Config) *Service {
	if strings.TrimSpace(config.AccountsFile) == "" {
		config.AccountsFile = DefaultAccountsPath()
	}
	if strings.TrimSpace(config.CacheDir) == "" {
		config.CacheDir = DefaultCacheDir()
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: 20 * time.Second}
	}
	if strings.TrimSpace(config.UserAgent) == "" {
		config.UserAgent = codexapi.DefaultUserAgent
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Service{
		accountsFile: config.AccountsFile, cacheDir: config.CacheDir,
		client: config.HTTPClient, userAgent: config.UserAgent, now: config.Now,
		browserSessions: make(map[string]cursorapi.LoginSession),
	}
}

// ListAccounts returns public account metadata sorted by display name.
func (service *Service) ListAccounts() ([]Account, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	store, err := service.loadAccounts()
	if err != nil {
		return nil, err
	}
	accounts := make([]Account, len(store.Accounts))
	for index := range store.Accounts {
		accounts[index] = store.Accounts[index].public()
	}
	sort.Slice(accounts, func(i, j int) bool { return strings.ToLower(accounts[i].Name) < strings.ToLower(accounts[j].Name) })
	return accounts, nil
}

// RenameAccount renames the account selected by ID, name, or email.
func (service *Service) RenameAccount(target, newName string) (AccountMutation, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return AccountMutation{}, fmt.Errorf("new account name cannot be empty")
	}
	store, err := service.loadAccounts()
	if err != nil {
		return AccountMutation{}, err
	}
	index, err := findAccount(store.Accounts, target)
	if err != nil {
		return AccountMutation{}, err
	}
	for other := range store.Accounts {
		if other != index && strings.EqualFold(strings.TrimSpace(store.Accounts[other].Name), newName) {
			return AccountMutation{}, fmt.Errorf("account name %q already exists", newName)
		}
	}
	store.Accounts[index].Name = newName
	if err := service.saveAccounts(store); err != nil {
		return AccountMutation{}, err
	}
	return AccountMutation{Action: "renamed", Account: store.Accounts[index].public()}, nil
}

// RemoveAccount deletes an account and its CLI usage cache.
func (service *Service) RemoveAccount(target string) (AccountMutation, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	store, err := service.loadAccounts()
	if err != nil {
		return AccountMutation{}, err
	}
	index, err := findAccount(store.Accounts, target)
	if err != nil {
		return AccountMutation{}, err
	}
	removed := store.Accounts[index]
	store.Accounts = append(store.Accounts[:index], store.Accounts[index+1:]...)
	if err := service.saveAccounts(store); err != nil {
		return AccountMutation{}, err
	}
	if err := os.Remove(service.cachePath(removed.ID)); err != nil && !os.IsNotExist(err) {
		return AccountMutation{}, err
	}
	return AccountMutation{Action: "removed", Account: removed.public()}, nil
}

// Settings returns the normalized dashboard settings.
func (service *Service) Settings() (Settings, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.loadSettings()
}

// UpdateSettings normalizes and replaces the dashboard settings.
func (service *Service) UpdateSettings(settings Settings) (Settings, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	normalizeSettings(&settings)
	if err := service.saveSettings(settings); err != nil {
		return Settings{}, err
	}
	return settings, nil
}

// BeginDeviceAuth starts device authorization for an explicit provider.
func (service *Service) BeginDeviceAuth(ctx context.Context, provider string) (DeviceAuthSession, error) {
	if provider != providerCodex {
		return DeviceAuthSession{}, fmt.Errorf("provider %q does not support device authentication", provider)
	}
	response, err := codexapi.RequestDeviceUserCode(ctx, service.client)
	if err != nil {
		return DeviceAuthSession{}, err
	}
	interval, err := strconv.Atoi(strings.TrimSpace(response.Interval))
	if err != nil || interval < 1 {
		interval = 5
	}
	return DeviceAuthSession{
		Provider:  providerCodex,
		SessionID: response.DeviceAuthID, UserCode: response.UserCode,
		VerificationURL: codexapi.DeviceVerificationURL, PollIntervalSeconds: interval,
	}, nil
}

// PollDeviceAuth polls once and persists credentials when authorization completes.
func (service *Service) PollDeviceAuth(ctx context.Context, request DeviceAuthPoll) (DeviceAuthResult, error) {
	if request.Provider != providerCodex {
		return DeviceAuthResult{}, fmt.Errorf("provider %q does not support device authentication", request.Provider)
	}
	if strings.TrimSpace(request.SessionID) == "" || strings.TrimSpace(request.UserCode) == "" {
		return DeviceAuthResult{}, fmt.Errorf("session_id and user_code are required")
	}
	poll, pending, err := codexapi.PollDeviceToken(ctx, service.client, request.SessionID, request.UserCode)
	if err != nil {
		return DeviceAuthResult{}, err
	}
	if pending {
		return DeviceAuthResult{Status: "pending"}, nil
	}
	tokens, err := codexapi.ExchangeAuthorizationCode(ctx, service.client, poll.AuthorizationCode, codexapi.DeviceRedirectURI, poll.CodeVerifier)
	if err != nil {
		return DeviceAuthResult{}, err
	}
	if tokens.IDToken == "" || tokens.RefreshToken == "" {
		return DeviceAuthResult{}, fmt.Errorf("token exchange response missing required tokens")
	}
	identity, err := codexapi.ParseIdentity(tokens.IDToken)
	if err != nil {
		return DeviceAuthResult{}, err
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		name = defaultAccountName(identity.Email, service.now())
	}
	candidate := storedAccount{
		ID: newAccountID(service.now()), Name: name, Provider: providerCodex,
		Email: stringPointer(identity.Email), PlanType: stringPointer(identity.PlanType),
		AuthData: authData{
			Type: "chatgpt", IDToken: &tokens.IDToken, AccessToken: &tokens.AccessToken,
			RefreshToken: &tokens.RefreshToken, AccountID: stringPointer(identity.AccountID),
		},
	}
	return service.saveLogin(candidate, strings.TrimSpace(request.Name) != "")
}

// BeginOAuthAuth starts a browser login for providers that use a pasted authorization code.
func (service *Service) BeginOAuthAuth(provider string) (OAuthSession, error) {
	if provider != providerClaude {
		return OAuthSession{}, fmt.Errorf("provider %q does not support browser code authentication", provider)
	}
	session, err := claudeapi.NewAuthSession()
	if err != nil {
		return OAuthSession{}, err
	}
	return OAuthSession{Provider: providerClaude, SessionID: session.SessionID(), VerificationURL: session.URL}, nil
}

// CompleteOAuthAuth exchanges the pasted code and persists the account.
func (service *Service) CompleteOAuthAuth(ctx context.Context, request OAuthComplete) (DeviceAuthResult, error) {
	if request.Provider != providerClaude {
		return DeviceAuthResult{}, fmt.Errorf("provider %q does not support browser code authentication", request.Provider)
	}
	session, err := claudeapi.ParseSessionID(request.SessionID)
	if err != nil {
		return DeviceAuthResult{}, err
	}
	login, err := claudeapi.CompleteLogin(ctx, service.client, session, request.Code, service.now())
	if err != nil {
		return DeviceAuthResult{}, err
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		name = defaultAccountName(login.Email, service.now())
	}
	candidate := storedAccount{
		ID: newAccountID(service.now()), Name: name, Provider: providerClaude,
		Email: stringPointer(login.Email), PlanType: stringPointer(login.Plan),
		AuthData: authData{Type: "claude_oauth", AccountID: stringPointer(login.AccountUUID)},
	}
	setClaudeCredentials(&candidate, login.Credentials)
	return service.saveLogin(candidate, strings.TrimSpace(request.Name) != "")
}

func (service *Service) saveLogin(candidate storedAccount, explicitName bool) (DeviceAuthResult, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	store, err := service.loadAccounts()
	if err != nil {
		return DeviceAuthResult{}, err
	}
	action := "added"
	if index := matchingLogin(store.Accounts, candidate, explicitName); index >= 0 {
		candidate.ID = store.Accounts[index].ID
		if !explicitName {
			candidate.Name = store.Accounts[index].Name
		}
		store.Accounts[index] = candidate
		action = "updated"
	} else {
		store.Accounts = append(store.Accounts, candidate)
	}
	if err := service.saveAccounts(store); err != nil {
		return DeviceAuthResult{}, err
	}
	account := candidate.public()
	return DeviceAuthResult{Status: "complete", Action: action, Account: &account}, nil
}

// SaveAPIKeyAccount creates an API-key account for a supported provider.
func (service *Service) SaveAPIKeyAccount(request APIKeyAccount) (AccountMutation, error) {
	provider, ok := providerDefinitions[request.Provider]
	if !ok || provider.Credentials != apiKeyCredentials {
		return AccountMutation{}, fmt.Errorf("provider %q does not support API-key accounts", request.Provider)
	}
	if strings.TrimSpace(request.APIKey) == "" {
		return AccountMutation{}, fmt.Errorf("api_key is required")
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		name = provider.Name
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	store, err := service.loadAccounts()
	if err != nil {
		return AccountMutation{}, err
	}
	if strings.TrimSpace(request.Account) != "" {
		index, err := findAccount(store.Accounts, request.Account)
		if err != nil {
			return AccountMutation{}, err
		}
		if store.Accounts[index].Provider != provider.ID {
			return AccountMutation{}, fmt.Errorf("account %q does not use provider %s", store.Accounts[index].Name, provider.Name)
		}
		store.Accounts[index].AuthData = authData{Type: string(apiKeyCredentials), APIKey: stringPointer(strings.TrimSpace(request.APIKey))}
		if err := service.saveAccounts(store); err != nil {
			return AccountMutation{}, err
		}
		if err := service.clearCache(store.Accounts[index].ID); err != nil {
			return AccountMutation{}, err
		}
		return AccountMutation{Action: "updated", Account: store.Accounts[index].public()}, nil
	}
	baseName := name
	for suffix := 2; accountNameExists(store.Accounts, name); suffix++ {
		name = fmt.Sprintf("%s %d", baseName, suffix)
	}
	account := storedAccount{ID: newAccountID(service.now()), Name: name, Provider: provider.ID, PlanType: stringPointer(provider.Plan), AuthData: authData{Type: string(apiKeyCredentials), APIKey: stringPointer(strings.TrimSpace(request.APIKey))}}
	store.Accounts = append(store.Accounts, account)
	if err := service.saveAccounts(store); err != nil {
		return AccountMutation{}, err
	}
	return AccountMutation{Action: "added", Account: account.public()}, nil
}

func accountNameExists(accounts []storedAccount, name string) bool {
	for _, account := range accounts {
		if strings.EqualFold(strings.TrimSpace(account.Name), strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}

// Usage returns one account or all accounts when target is empty. When refresh
// is false it performs no provider requests and only reads cached data.
func (service *Service) Usage(ctx context.Context, target string, refresh bool) ([]UsageResult, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	store, err := service.loadAccounts()
	if err != nil {
		return nil, err
	}
	indexes, err := accountIndexes(store.Accounts, target)
	if err != nil {
		return nil, err
	}
	results := make([]UsageResult, 0, len(indexes))
	accountsChanged := false
	for _, index := range indexes {
		account := &store.Accounts[index]
		result, changed, err := service.usageForAccount(ctx, account, refresh)
		if err != nil {
			result = UsageResult{Account: account.public(), Error: err.Error()}
		}
		accountsChanged = accountsChanged || changed
		results = append(results, result)
	}
	if accountsChanged {
		if err := service.saveAccounts(store); err != nil {
			return nil, err
		}
	}
	return results, nil
}

// ResetCredits returns available credits for one account, optionally using cache only.
func (service *Service) ResetCredits(ctx context.Context, target string, refresh bool) (ResetCreditsResult, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	store, err := service.loadAccounts()
	if err != nil {
		return ResetCreditsResult{}, err
	}
	index, err := findAccount(store.Accounts, target)
	if err != nil {
		return ResetCreditsResult{}, err
	}
	account := &store.Accounts[index]
	provider, ok := providerDefinitions[account.Provider]
	if !ok || !provider.ResetCredits {
		return ResetCreditsResult{}, fmt.Errorf("reset credits are unavailable for %s", account.Provider)
	}
	entry, cached, err := service.loadCache(account.ID)
	if err != nil {
		return ResetCreditsResult{}, err
	}
	if !refresh {
		if !cached || entry.ResetCredits == nil {
			return ResetCreditsResult{}, fmt.Errorf("no cached reset credits for account %q", account.Name)
		}
		return ResetCreditsResult{Account: account.public(), Credits: publicResetCredits(entry.ResetCredits), Cached: true}, nil
	}
	changed, err := service.refreshCredentials(ctx, account)
	if err != nil {
		return ResetCreditsResult{}, err
	}
	if changed {
		if err := service.saveAccounts(store); err != nil {
			return ResetCreditsResult{}, err
		}
	}
	credits, err := codexapi.FetchResetCredits(ctx, service.client, stringValue(account.AuthData.AccessToken), stringValue(account.AuthData.AccountID), service.userAgent)
	if err != nil {
		return ResetCreditsResult{}, err
	}
	now := service.now()
	entry.ResetCredits, entry.ResetFetchedAt = credits, now.Unix()
	if err := service.saveCache(account.ID, entry); err != nil {
		return ResetCreditsResult{}, err
	}
	return ResetCreditsResult{Account: account.public(), Credits: publicResetCredits(credits)}, nil
}

func (service *Service) usageForAccount(ctx context.Context, account *storedAccount, refresh bool) (UsageResult, bool, error) {
	provider, ok := providerDefinitions[account.Provider]
	if !ok {
		return UsageResult{}, false, fmt.Errorf("unsupported provider %q", account.Provider)
	}
	if provider.Credentials == apiKeyCredentials {
		entry, cached, err := service.loadCache(account.ID)
		if err != nil {
			return UsageResult{}, false, err
		}
		if !refresh {
			if !cached || entry.ProviderUsage == nil {
				return UsageResult{}, false, fmt.Errorf("no cached usage for account %q", account.Name)
			}
			usage := *entry.ProviderUsage
			resultAccount := account.public()
			if usage.Plan != "" {
				resultAccount.PlanType = usage.Plan
			}
			return UsageResult{Account: resultAccount, Metrics: publicMetrics(usage.Metrics), Cached: true}, false, nil
		}
		var usage providers.Usage
		changed := false
		if account.Provider == providerCursor {
			var token string
			usage, token, err = providers.FetchCursorUsage(ctx, service.client, stringValue(account.AuthData.APIKey), stringValue(account.AuthData.AccessToken), service.now())
			if err == nil && token != stringValue(account.AuthData.AccessToken) {
				account.AuthData.AccessToken = stringPointer(token)
				changed = true
			}
		} else {
			usage, err = fetchAPIKeyUsage(ctx, service.client, *account, service.userAgent)
		}
		if err != nil {
			if cached && entry.ProviderUsage != nil {
				cachedUsage := *entry.ProviderUsage
				resultAccount := account.public()
				if cachedUsage.Plan != "" {
					resultAccount.PlanType = cachedUsage.Plan
				}
				return UsageResult{Account: resultAccount, Metrics: publicMetrics(cachedUsage.Metrics), Cached: true, Error: err.Error()}, false, nil
			}
			return UsageResult{}, false, err
		}
		now := service.now()
		entry.ProviderUsage, entry.FetchedAt = &usage, now.Unix()
		if usage.Plan != "" && stringValue(account.PlanType) != usage.Plan {
			account.PlanType = &usage.Plan
			changed = true
		}
		if err := service.saveCache(account.ID, entry); err != nil {
			return UsageResult{}, false, err
		}
		return UsageResult{Account: account.public(), Metrics: publicMetrics(usage.Metrics)}, changed, nil
	}
	if provider.ID == providerClaude {
		return service.claudeUsage(ctx, account, refresh)
	}
	entry, cached, err := service.loadCache(account.ID)
	if err != nil {
		return UsageResult{}, false, err
	}
	if !refresh {
		result, ok := cachedUsageResult(account, entry, cached)
		if !ok {
			return UsageResult{}, false, fmt.Errorf("no cached usage for account %q", account.Name)
		}
		return result, false, nil
	}

	changed, err := service.refreshCredentials(ctx, account)
	if err != nil {
		if result, ok := cachedUsageResult(account, entry, cached); ok {
			result.Error = err.Error()
			return result, false, nil
		}
		return UsageResult{}, false, err
	}
	accessToken, accountID := stringValue(account.AuthData.AccessToken), stringValue(account.AuthData.AccountID)
	var usage *codexapi.RateLimitStatusPayload
	var credits *codexapi.ResetCreditsPayload
	var usageErr, creditsErr error
	var requests sync.WaitGroup
	requests.Add(2)
	go func() {
		defer requests.Done()
		usage, usageErr = codexapi.FetchUsage(ctx, service.client, accessToken, accountID, service.userAgent)
	}()
	go func() {
		defer requests.Done()
		credits, creditsErr = codexapi.FetchResetCredits(ctx, service.client, accessToken, accountID, service.userAgent)
	}()
	requests.Wait()
	if usageErr != nil {
		if result, ok := cachedUsageResult(account, entry, cached); ok {
			result.Error = usageErr.Error()
			return result, changed, nil
		}
		return UsageResult{}, changed, usageErr
	}
	now := service.now()
	snapshotCredits, snapshotCreditsErr := credits, creditsErr
	if creditsErr != nil && entry.ResetCredits != nil {
		snapshotCredits, snapshotCreditsErr = entry.ResetCredits, nil
	}
	snapshot := codexapi.BuildSnapshot(usage, snapshotCredits, snapshotCreditsErr, now)
	if creditsErr != nil {
		snapshot.CreditsError = "Reset credits unavailable"
	}
	entry.PlanType, entry.RateLimit, entry.FetchedAt = usage.PlanType, usage.RateLimit, now.Unix()
	providerUsage := codexProviderUsage(usage)
	entry.ProviderUsage = &providerUsage
	if creditsErr == nil {
		entry.ResetCredits, entry.ResetFetchedAt = credits, now.Unix()
	}
	if err := service.saveCache(account.ID, entry); err != nil {
		return UsageResult{}, changed, err
	}
	if usage.PlanType != "" && stringValue(account.PlanType) != usage.PlanType {
		account.PlanType = &usage.PlanType
		changed = true
	}
	return UsageResult{Account: account.public(), Snapshot: publicSnapshot(snapshot), Metrics: publicMetrics(providerUsage.Metrics)}, changed, nil
}

func (service *Service) claudeUsage(ctx context.Context, account *storedAccount, refresh bool) (UsageResult, bool, error) {
	entry, cached, err := service.loadCache(account.ID)
	if err != nil {
		return UsageResult{}, false, err
	}
	cachedResult := func() (UsageResult, bool) {
		if !cached || entry.ProviderUsage == nil {
			return UsageResult{}, false
		}
		return UsageResult{Account: account.public(), Metrics: publicMetrics(entry.ProviderUsage.Metrics), Cached: true}, true
	}
	now := service.now()
	// The usage endpoint is rate limited: within the throttle window (shared
	// with the dashboard through the cache) serve the cache instead.
	if !refresh || entry.NextFetchAt > now.Unix() {
		if result, ok := cachedResult(); ok {
			return result, false, nil
		}
		if !refresh {
			return UsageResult{}, false, fmt.Errorf("no cached usage for account %q", account.Name)
		}
	}
	credentials := claudeapi.Credentials{AccessToken: stringValue(account.AuthData.AccessToken), RefreshToken: stringValue(account.AuthData.RefreshToken)}
	if account.AuthData.ExpiresAt != nil {
		credentials.ExpiresAt = *account.AuthData.ExpiresAt
	}
	fetchPlan := providers.ClaudePlanDue(stringValue(account.PlanType), time.Unix(entry.PlanCheckedAt, 0), now)
	fetched, err := providers.FetchClaudeUsage(ctx, service.client, credentials, now, fetchPlan)
	changed := fetched.CredentialsChanged
	if changed {
		setClaudeCredentials(account, fetched.Credentials)
	}
	if err != nil {
		if delay, limited := claudeapi.RateLimitDelay(err); limited {
			entry.NextFetchAt = now.Add(delay).Unix()
			if saveErr := service.saveCache(account.ID, entry); saveErr != nil {
				return UsageResult{}, changed, saveErr
			}
		}
		if result, ok := cachedResult(); ok {
			result.Error = err.Error()
			return result, changed, nil
		}
		return UsageResult{}, changed, err
	}
	usage := fetched.Usage
	usage.Plan = firstNonEmpty(usage.Plan, stringValue(account.PlanType))
	entry.PlanType, entry.ProviderUsage, entry.FetchedAt = usage.Plan, &usage, now.Unix()
	entry.NextFetchAt = now.Add(claudeapi.MinRefreshInterval).Unix()
	if fetched.PlanChecked {
		entry.PlanCheckedAt = now.Unix()
	}
	if err := service.saveCache(account.ID, entry); err != nil {
		return UsageResult{}, changed, err
	}
	if usage.Plan != "" && stringValue(account.PlanType) != usage.Plan {
		account.PlanType = &usage.Plan
		changed = true
	}
	return UsageResult{Account: account.public(), Metrics: publicMetrics(usage.Metrics)}, changed, nil
}

func setClaudeCredentials(account *storedAccount, credentials claudeapi.Credentials) {
	account.AuthData.AccessToken = stringPointer(credentials.AccessToken)
	account.AuthData.RefreshToken = stringPointer(credentials.RefreshToken)
	account.AuthData.ExpiresAt = nil
	if credentials.ExpiresAt > 0 {
		expiresAt := credentials.ExpiresAt
		account.AuthData.ExpiresAt = &expiresAt
	}
}

func cachedUsageResult(account *storedAccount, entry cacheEntry, exists bool) (UsageResult, bool) {
	if !exists || entry.FetchedAt <= 0 || entry.ProviderUsage == nil {
		return UsageResult{}, false
	}
	creditsErr := error(nil)
	if entry.ResetCredits == nil {
		creditsErr = fmt.Errorf("reset credits not cached")
	}
	snapshot := codexapi.BuildSnapshot(
		&codexapi.RateLimitStatusPayload{PlanType: entry.PlanType, RateLimit: entry.RateLimit},
		entry.ResetCredits, creditsErr, time.Unix(entry.FetchedAt, 0),
	)
	usage := *entry.ProviderUsage
	return UsageResult{Account: account.public(), Snapshot: publicSnapshot(snapshot), Metrics: publicMetrics(usage.Metrics), Cached: true}, true
}

func (service *Service) refreshCredentials(ctx context.Context, account *storedAccount) (bool, error) {
	if account.AuthData.AccessToken == nil || account.AuthData.RefreshToken == nil {
		return false, fmt.Errorf("account %q is missing credentials", account.Name)
	}
	credentials := codexapi.Credentials{
		AccessToken: stringValue(account.AuthData.AccessToken), RefreshToken: stringValue(account.AuthData.RefreshToken),
		IDToken: stringValue(account.AuthData.IDToken), AccountID: stringValue(account.AuthData.AccountID),
	}
	updated, changed, err := codexapi.RefreshCredentials(ctx, service.client, credentials, service.now())
	if err != nil {
		return false, err
	}
	if changed {
		account.AuthData.AccessToken = &updated.AccessToken
		account.AuthData.RefreshToken = &updated.RefreshToken
		account.AuthData.IDToken = stringPointer(updated.IDToken)
	}
	return changed, nil
}

func findAccount(accounts []storedAccount, target string) (int, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return -1, fmt.Errorf("account target cannot be empty")
	}
	matches := make([]int, 0, 1)
	for index := range accounts {
		if accounts[index].ID == target || strings.EqualFold(strings.TrimSpace(accounts[index].Name), target) || strings.EqualFold(stringValue(accounts[index].Email), target) {
			matches = append(matches, index)
		}
	}
	if len(matches) == 0 {
		return -1, fmt.Errorf("account not found: %s", target)
	}
	if len(matches) > 1 {
		return -1, fmt.Errorf("multiple accounts match %q; use account ID", target)
	}
	return matches[0], nil
}

func accountIndexes(accounts []storedAccount, target string) ([]int, error) {
	if strings.TrimSpace(target) != "" {
		index, err := findAccount(accounts, target)
		return []int{index}, err
	}
	indexes := make([]int, len(accounts))
	for index := range accounts {
		indexes[index] = index
	}
	return indexes, nil
}

func matchingLogin(accounts []storedAccount, candidate storedAccount, explicitName bool) int {
	for index := range accounts {
		if accounts[index].Provider != candidate.Provider {
			continue
		}
		matches := candidate.Email != nil && accounts[index].Email != nil && strings.EqualFold(*accounts[index].Email, *candidate.Email)
		if candidate.Provider == providerCursor && stringValue(candidate.AuthData.AccountID) != "" && stringValue(candidate.AuthData.AccountID) == stringValue(accounts[index].AuthData.AccountID) {
			matches = true
		}
		if !matches {
			continue
		}
		if explicitName && !strings.EqualFold(accounts[index].Name, candidate.Name) {
			continue
		}
		if accounts[index].PlanType != nil && candidate.PlanType != nil && !strings.EqualFold(*accounts[index].PlanType, *candidate.PlanType) {
			continue
		}
		return index
	}
	return -1
}

func defaultAccountName(email string, now time.Time) string {
	if at := strings.Index(email, "@"); at > 0 {
		return email[:at]
	}
	if strings.TrimSpace(email) != "" {
		return email
	}
	return fmt.Sprintf("account-%d", now.Unix())
}

func newAccountID(now time.Time) string {
	bytes := make([]byte, 12)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("acc-%d", now.UnixNano())
	}
	return hex.EncodeToString(bytes)
}

func creditAvailable(credit codexapi.ResetCreditDetail) bool {
	return strings.EqualFold(strings.TrimSpace(credit.Status), "available")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

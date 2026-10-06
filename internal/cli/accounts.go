package cli

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/McMelonTV/check-usage/internal/login"
	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/internal/storage"
	"github.com/McMelonTV/check-usage/internal/usage"
)

func runAccountsCommand(args []string) int {
	if len(args) == 0 {
		printAccountsCommandUsage()
		return 1
	}

	switch args[0] {
	case "list":
		return runAccountsList(args[1:])
	case "add":
		return runAccountsAdd(args[1:])
	case "login":
		return runAccountsLogin(args[1:])
	case "reauth":
		return runAccountsReauth(args[1:])
	case "remove":
		return runAccountsRemove(args[1:])
	case "rename":
		return runAccountsRename(args[1:])
	case "help", "-h", "--help":
		printAccountsCommandUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown accounts command: %s\n", args[0])
		printAccountsCommandUsage()
		return 1
	}
}

func accountProviderFlag(fs *flag.FlagSet) *string {
	return fs.String("provider", "", "provider: codex, claude, opencode-go, deepseek, or cursor")
}

func selectedProvider(id string) (usage.ProviderDefinition, error) {
	if strings.TrimSpace(id) == "" {
		return usage.ProviderDefinition{}, fmt.Errorf("--provider is required; choose: codex, claude, opencode-go, deepseek, cursor")
	}
	return usage.ProviderFor(id)
}

func runAccountsAdd(args []string) int {
	fs := flag.NewFlagSet("accounts add", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	setDoubleDashFlagUsage(fs)
	accountsPath := fs.String("accounts-file", storage.DefaultAccountsPath(), "path to accounts.json")
	providerID := accountProviderFlag(fs)
	name := fs.String("name", "", "display name for the account")
	key := fs.String("api-key", "", "API key for the selected provider")
	keyEnv := fs.String("api-key-env", "", "environment variable containing the API key")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "accounts add does not take positional arguments")
		return 2
	}
	provider, err := selectedProvider(*providerID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}
	if provider.Credentials != providers.APIKey {
		fmt.Fprintf(os.Stderr, "error: %s uses browser login; run accounts login --provider %s\n", provider.Name, provider.ID)
		return 2
	}
	resolvedKey, err := resolveAPIKey(*key, *keyEnv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}
	if resolvedKey == "" {
		fmt.Fprintln(os.Stderr, "error: --api-key cannot be empty")
		return 2
	}
	accountName := strings.TrimSpace(*name)
	if accountName == "" {
		accountName = provider.Name
	}
	store, err := storage.LoadAccountsOrEmpty(*accountsPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	for _, account := range store.Accounts {
		if strings.EqualFold(strings.TrimSpace(account.Name), accountName) {
			fmt.Fprintf(os.Stderr, "error: account name %q already exists\n", accountName)
			return 1
		}
	}
	store.Accounts = append(store.Accounts, storage.Account{ID: storage.NewAccountID(), Name: accountName, Provider: provider.ID, PlanType: storage.OptionalString(provider.Plan), AuthData: storage.AuthData{Type: string(providers.APIKey), APIKey: new(resolvedKey)}})
	if err := storage.SaveAccounts(*accountsPath, store); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	fmt.Printf("Added %s account %q.\n", provider.Name, accountName)
	return 0
}

func runAccountsReauth(args []string) int {
	fs := flag.NewFlagSet("accounts reauth", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	setDoubleDashFlagUsage(fs)
	accountsPath := fs.String("accounts-file", storage.DefaultAccountsPath(), "path to accounts.json")
	apiKey := fs.String("api-key", "", "new API key for API-key providers")
	apiKeyEnv := fs.String("api-key-env", "", "environment variable containing the new API key")
	timeout := fs.Int("timeout", 30, "HTTP timeout in seconds")
	noBrowser := fs.Bool("no-browser", false, "do not open browser automatically")
	authFlow := fs.String("auth-flow", "device", "authentication flow: device or browser")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: accounts reauth [--accounts-file path] [--api-key key|--api-key-env name] [--timeout seconds] [--no-browser] [--auth-flow device|browser] <id-or-name>")
		return 2
	}

	store, err := storage.LoadAccountsOrEmpty(*accountsPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	index, err := findAccountForRemoval(store.Accounts, strings.TrimSpace(fs.Arg(0)))
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	existing := store.Accounts[index]

	provider, err := usage.ProviderFor(existing.Provider)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if provider.Credentials == providers.APIKey && (provider.ID != providers.Cursor || flagsSet(fs, "api-key", "api-key-env")) {
		if flagsSet(fs, "timeout", "no-browser", "auth-flow") {
			fmt.Fprintf(os.Stderr, "error: browser authentication flags do not apply to %s\n", provider.Name)
			return 2
		}
		key, keyErr := resolveAPIKey(*apiKey, *apiKeyEnv)
		if keyErr != nil {
			fmt.Fprintln(os.Stderr, "error:", keyErr)
			return 2
		}
		if key == "" {
			fmt.Fprintf(os.Stderr, "error: %s requires --api-key\n", provider.Name)
			return 2
		}
		existing.AuthData = storage.AuthData{Type: string(providers.APIKey), APIKey: new(key)}
		store.Accounts[index] = existing
		if err := storage.SaveAccounts(*accountsPath, store); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		if err := storage.RemoveAccountCache(existing.ID); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		fmt.Printf("Updated API key for %q (%s).\n", existing.Name, existing.ID)
		return 0
	}
	if flagsSet(fs, "api-key", "api-key-env") {
		fmt.Fprintf(os.Stderr, "error: API-key flags do not apply to %s\n", provider.Name)
		return 2
	}

	client := &http.Client{Timeout: time.Duration(*timeout) * time.Second}
	var refreshed storage.Account
	flow := strings.ToLower(strings.TrimSpace(*authFlow))
	if provider.Credentials == providers.OAuthCode {
		if flagsSet(fs, "auth-flow") {
			fmt.Fprintf(os.Stderr, "error: --auth-flow does not apply to %s\n", provider.Name)
			return 2
		}
		flow = "code"
	}
	if provider.ID == providers.Cursor {
		if flagsSet(fs, "auth-flow") && flow != "browser" && flow != "oauth" {
			fmt.Fprintln(os.Stderr, "error: Cursor uses browser authentication")
			return 2
		}
		flow = "cursor"
	}
	switch flow {
	case "cursor":
		refreshed, err = login.RunCursorBrowser(existing.Name, client, !*noBrowser)
	case "code":
		refreshed, err = login.RunClaude(existing.Name, client, !*noBrowser)
	case "device":
		refreshed, err = login.RunCodexDevice(existing.Name, client, !*noBrowser)
	case "browser", "oauth":
		refreshed, err = login.RunCodexBrowser(existing.Name, client, !*noBrowser)
	default:
		fmt.Fprintln(os.Stderr, "error: --auth-flow must be one of: device, browser")
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if existing.Email != nil && refreshed.Email != nil && !strings.EqualFold(strings.TrimSpace(*existing.Email), strings.TrimSpace(*refreshed.Email)) {
		fmt.Fprintln(os.Stderr, "error: signed-in email does not match the selected account")
		return 1
	}
	if existing.Email != nil && refreshed.Email == nil {
		fmt.Fprintln(os.Stderr, "error: signed-in account did not provide the expected email")
		return 1
	}
	if existing.AuthData.AccountID != nil && (refreshed.AuthData.AccountID == nil || strings.TrimSpace(*existing.AuthData.AccountID) != strings.TrimSpace(*refreshed.AuthData.AccountID)) {
		fmt.Fprintln(os.Stderr, "error: signed-in account does not match the selected account ID")
		return 1
	}

	refreshed.ID, refreshed.Name = existing.ID, existing.Name
	refreshed.Provider = existing.Provider
	store.Accounts[index] = refreshed
	if err := storage.SaveAccounts(*accountsPath, store); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if err := storage.RemoveAccountCache(existing.ID); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	fmt.Printf("Reauthenticated account %q (%s).\n", refreshed.Name, refreshed.ID)
	return 0
}

func resolveAPIKey(value, environmentVariable string) (string, error) {
	value, environmentVariable = strings.TrimSpace(value), strings.TrimSpace(environmentVariable)
	if value != "" && environmentVariable != "" {
		return "", fmt.Errorf("use only one of --api-key or --api-key-env")
	}
	if environmentVariable != "" {
		value = strings.TrimSpace(os.Getenv(environmentVariable))
		if value == "" {
			return "", fmt.Errorf("environment variable %s is empty or unset", environmentVariable)
		}
	}
	return value, nil
}

func flagsSet(set *flag.FlagSet, names ...string) bool {
	selected := make(map[string]bool, len(names))
	for _, name := range names {
		selected[name] = true
	}
	found := false
	set.Visit(func(flag *flag.Flag) {
		found = found || selected[flag.Name]
	})
	return found
}

func runAccountsList(args []string) int {
	fs := flag.NewFlagSet("accounts list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	setDoubleDashFlagUsage(fs)
	accountsPath := fs.String("accounts-file", storage.DefaultAccountsPath(), "path to accounts.json")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "accounts list does not take positional arguments")
		return 2
	}

	store, err := storage.LoadAccountsOrEmpty(*accountsPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	printAccountsList(store.Accounts)
	return 0
}

func runAccountsLogin(args []string) int {
	fs := flag.NewFlagSet("accounts login", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	setDoubleDashFlagUsage(fs)
	accountsPath := fs.String("accounts-file", storage.DefaultAccountsPath(), "path to accounts.json")
	providerID := accountProviderFlag(fs)
	name := fs.String("name", "", "display name for the account")
	timeout := fs.Int("timeout", 30, "HTTP timeout in seconds")
	noBrowser := fs.Bool("no-browser", false, "do not open browser automatically")
	authFlow := fs.String("auth-flow", "device", "authentication flow: device or browser")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "accounts login does not take positional arguments")
		return 2
	}
	requestedName := strings.TrimSpace(*name)
	provider, err := selectedProvider(*providerID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}
	if provider.Credentials == providers.APIKey && provider.ID != providers.Cursor {
		fmt.Fprintf(os.Stderr, "error: %s uses an API key; run accounts add --provider %s --api-key key\n", provider.Name, provider.ID)
		return 2
	}

	client := &http.Client{Timeout: time.Duration(*timeout) * time.Second}
	flow := strings.ToLower(strings.TrimSpace(*authFlow))
	recheckPlan := planRecheckFunc(recheckAccountPlanType)
	if provider.Credentials == providers.OAuthCode {
		if flagsSet(fs, "auth-flow") {
			fmt.Fprintf(os.Stderr, "error: --auth-flow does not apply to %s\n", provider.Name)
			return 2
		}
		flow, recheckPlan = "code", nil
	}
	if provider.ID == providers.Cursor {
		if flagsSet(fs, "auth-flow") && flow != "browser" && flow != "oauth" {
			fmt.Fprintln(os.Stderr, "error: Cursor uses browser authentication")
			return 2
		}
		flow, recheckPlan = "cursor", nil
	}
	var account storage.Account
	switch flow {
	case "cursor":
		account, err = login.RunCursorBrowser(requestedName, client, !*noBrowser)
	case "code":
		account, err = login.RunClaude(requestedName, client, !*noBrowser)
	case "device":
		account, err = login.RunCodexDevice(requestedName, client, !*noBrowser)
	case "browser", "oauth":
		account, err = login.RunCodexBrowser(requestedName, client, !*noBrowser)
	default:
		fmt.Fprintln(os.Stderr, "error: --auth-flow must be one of: device, browser")
		return 2
	}
	account.Provider = provider.ID
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	store, err := storage.LoadAccountsOrEmpty(*accountsPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	idx := storage.FindMatchingAccount(store.Accounts, account)
	if provider.ID == providers.Cursor && idx >= 0 && requestedName != "" && !strings.EqualFold(requestedName, store.Accounts[idx].Name) {
		idx = -1
	}
	if provider.ID == providers.Cursor && idx >= 0 {
		account.ID = store.Accounts[idx].ID
		if requestedName == "" {
			account.Name = store.Accounts[idx].Name
		}
		store.Accounts[idx] = account
		if err := storage.SaveAccounts(*accountsPath, store); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		if err := storage.RemoveAccountCache(account.ID); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		fmt.Printf("Updated account %q (%s).\n", account.Name, account.ID)
		return 0
	}
	if idx >= 0 {
		upsert, reason, existing, candidate := shouldUpsertMatchedAccount(store.Accounts[idx], account, requestedName, client, recheckPlan)
		store.Accounts[idx] = existing
		account = candidate
		if !upsert {
			fmt.Printf("Found account with same email but %s; adding as separate account.\n", reason)
		} else {
			account.ID = store.Accounts[idx].ID
			if requestedName == "" && strings.TrimSpace(store.Accounts[idx].Name) != "" {
				account.Name = store.Accounts[idx].Name
			}
			store.Accounts[idx] = account
			if err := storage.SaveAccounts(*accountsPath, store); err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				return 1
			}
			fmt.Printf("Updated account %q (%s).\n", account.Name, account.ID)
			return 0
		}
	}

	store.Accounts = append(store.Accounts, account)
	if err := storage.SaveAccounts(*accountsPath, store); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	fmt.Printf("Added account %q (%s).\n", account.Name, account.ID)
	return 0
}

func runAccountsRemove(args []string) int {
	fs := flag.NewFlagSet("accounts remove", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	setDoubleDashFlagUsage(fs)
	accountsPath := fs.String("accounts-file", storage.DefaultAccountsPath(), "path to accounts.json")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: accounts remove [--accounts-file path] <id-or-name>")
		return 2
	}
	target := strings.TrimSpace(fs.Arg(0))

	store, err := storage.LoadAccountsOrEmpty(*accountsPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if len(store.Accounts) == 0 {
		fmt.Println("No accounts to remove.")
		return 0
	}

	index, err := findAccountForRemoval(store.Accounts, target)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	removed := store.Accounts[index]
	store.Accounts = append(store.Accounts[:index], store.Accounts[index+1:]...)

	if err := storage.SaveAccounts(*accountsPath, store); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if err := storage.RemoveAccountCache(removed.ID); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	fmt.Printf("Removed account %q (%s).\n", removed.Name, removed.ID)
	return 0
}

func runAccountsRename(args []string) int {
	fs := flag.NewFlagSet("accounts rename", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	setDoubleDashFlagUsage(fs)
	accountsPath := fs.String("accounts-file", storage.DefaultAccountsPath(), "path to accounts.json")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "usage: accounts rename [--accounts-file path] <id-or-name> <new-name>")
		return 2
	}

	target := strings.TrimSpace(fs.Arg(0))
	newName := strings.TrimSpace(fs.Arg(1))
	if newName == "" {
		fmt.Fprintln(os.Stderr, "error: new account name cannot be empty")
		return 2
	}

	store, err := storage.LoadAccountsOrEmpty(*accountsPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if len(store.Accounts) == 0 {
		fmt.Println("No accounts to rename.")
		return 0
	}

	index, err := findAccountForRemoval(store.Accounts, target)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	for i := range store.Accounts {
		if i == index {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(store.Accounts[i].Name), newName) {
			fmt.Fprintf(os.Stderr, "error: account name %q already exists\n", newName)
			return 1
		}
	}

	oldName := store.Accounts[index].Name
	store.Accounts[index].Name = newName
	if err := storage.SaveAccounts(*accountsPath, store); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	fmt.Printf("Renamed account %q (%s) to %q.\n", oldName, store.Accounts[index].ID, newName)
	return 0
}

func printAccountsList(accounts []storage.Account) {
	if len(accounts) == 0 {
		fmt.Println("No accounts found.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tPROVIDER\tEMAIL\tPLAN\tAUTH TYPE")
	for _, acc := range accounts {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", acc.ID, acc.Name, acc.Provider, usage.ValueOrDash(acc.Email), usage.AccountPlan(acc), acc.AuthData.Type)
	}
	_ = w.Flush()
}

type planRecheckFunc func(storage.Account, *http.Client) (storage.Account, string, bool)

func shouldUpsertMatchedAccount(existing, candidate storage.Account, requestedName string, client *http.Client, recheckPlan planRecheckFunc) (bool, string, storage.Account, storage.Account) {
	requestedName = strings.TrimSpace(requestedName)
	if requestedName != "" && !strings.EqualFold(strings.TrimSpace(existing.Name), requestedName) {
		return false, "a different --name was provided", existing, candidate
	}

	existingPlan := normalizedPlanType(existing.PlanType)
	candidatePlan := normalizedPlanType(candidate.PlanType)
	if existingPlan == "" || candidatePlan == "" || existingPlan == candidatePlan || recheckPlan == nil {
		return true, "", existing, candidate
	}

	existing, existingPlan, existingChecked := recheckPlan(existing, client)
	candidate, candidatePlan, candidateChecked := recheckPlan(candidate, client)
	if existingChecked && candidateChecked && existingPlan != "" && candidatePlan != "" && existingPlan != candidatePlan {
		return false, "a different plan was detected after re-check", existing, candidate
	}

	return true, "", existing, candidate
}

func normalizedPlanType(plan *string) string {
	if plan == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(*plan))
}

func recheckAccountPlanType(account storage.Account, client *http.Client) (storage.Account, string, bool) {
	if client == nil {
		return account, "", false
	}

	refreshed, _, err := usage.EnsureFreshTokens(account, client)
	if err != nil {
		return account, "", false
	}
	account = refreshed

	usage, err := usage.FetchUsage(account, client)
	if err != nil {
		return account, "", false
	}

	plan := strings.TrimSpace(usage.PlanType)
	if plan == "" {
		return account, "", false
	}

	account.PlanType = new(plan)
	return account, strings.ToLower(plan), true
}

func findAccountForRemoval(accounts []storage.Account, target string) (int, error) {
	for i := range accounts {
		if accounts[i].ID == target {
			return i, nil
		}
	}

	nameMatches := make([]int, 0)
	for i := range accounts {
		if strings.EqualFold(accounts[i].Name, target) {
			nameMatches = append(nameMatches, i)
		}
	}

	if len(nameMatches) == 1 {
		return nameMatches[0], nil
	}
	if len(nameMatches) > 1 {
		return -1, fmt.Errorf("multiple accounts match name %q; remove by ID instead", target)
	}

	return -1, fmt.Errorf("account not found: %s", target)
}

func printAccountsCommandUsage() {
	fmt.Println("Usage:")
	fmt.Println("  check-usage accounts list [--accounts-file path]")
	fmt.Println("  check-usage accounts add [--accounts-file path] --provider opencode-go|deepseek|cursor (--api-key key|--api-key-env name) [--name name]")
	fmt.Println("  check-usage accounts login [--accounts-file path] --provider codex|claude|cursor [--name name] [--timeout seconds] [--no-browser] [--auth-flow device|browser]")
	fmt.Println("  check-usage accounts reauth [--accounts-file path] [--api-key key|--api-key-env name] [--timeout seconds] [--no-browser] [--auth-flow device|browser] <id-or-name>")
	fmt.Println("  check-usage accounts remove [--accounts-file path] <id-or-name>")
	fmt.Println("  check-usage accounts rename [--accounts-file path] <id-or-name> <new-name>")
}

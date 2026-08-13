package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"
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
	return fs.String("provider", "", "provider: codex, opencode-go, or deepseek")
}

func selectedProvider(id string) (providerDefinition, error) {
	if strings.TrimSpace(id) == "" {
		return providerDefinition{}, fmt.Errorf("--provider is required; choose: codex, opencode-go, deepseek")
	}
	return providerFor(id)
}

func runAccountsAdd(args []string) int {
	fs := flag.NewFlagSet("accounts add", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	setDoubleDashFlagUsage(fs)
	accountsPath := fs.String("accounts-file", defaultAccountsPath(), "path to accounts.json")
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
	if provider.Credentials != apiKeyCredentials {
		fmt.Fprintf(os.Stderr, "error: %s uses device login; run accounts login --provider %s\n", provider.Name, provider.ID)
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
	store, err := loadAccountsOrEmpty(*accountsPath)
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
	store.Accounts = append(store.Accounts, storedAccount{ID: newAccountID(), Name: accountName, Provider: provider.ID, PlanType: optionalString(provider.Plan), AuthData: authData{Type: string(apiKeyCredentials), APIKey: strPtr(resolvedKey)}})
	if err := saveAccounts(*accountsPath, store); err != nil {
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
	accountsPath := fs.String("accounts-file", defaultAccountsPath(), "path to accounts.json")
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

	store, err := loadAccountsOrEmpty(*accountsPath)
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

	provider, err := providerFor(existing.Provider)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if provider.Credentials == apiKeyCredentials {
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
		existing.AuthData = authData{Type: string(apiKeyCredentials), APIKey: strPtr(key)}
		store.Accounts[index] = existing
		if err := saveAccounts(*accountsPath, store); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		if err := removeAccountUsageCache(existing.ID); err != nil {
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
	var refreshed storedAccount
	switch strings.ToLower(strings.TrimSpace(*authFlow)) {
	case "device":
		refreshed, err = runDeviceAuthLogin(existing.Name, client, !*noBrowser)
	case "browser", "oauth":
		refreshed, err = runOAuthLogin(existing.Name, client, !*noBrowser)
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
	if err := saveAccounts(*accountsPath, store); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if err := removeAccountUsageCache(existing.ID); err != nil {
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
	accountsPath := fs.String("accounts-file", defaultAccountsPath(), "path to accounts.json")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "accounts list does not take positional arguments")
		return 2
	}

	store, err := loadAccountsOrEmpty(*accountsPath)
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
	accountsPath := fs.String("accounts-file", defaultAccountsPath(), "path to accounts.json")
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
	if provider.Credentials != deviceCredentials {
		fmt.Fprintf(os.Stderr, "error: %s uses an API key; run accounts add --provider %s --api-key key\n", provider.Name, provider.ID)
		return 2
	}

	client := &http.Client{Timeout: time.Duration(*timeout) * time.Second}
	flow := strings.ToLower(strings.TrimSpace(*authFlow))
	var account storedAccount
	switch flow {
	case "device":
		account, err = runDeviceAuthLogin(requestedName, client, !*noBrowser)
	case "browser", "oauth":
		account, err = runOAuthLogin(requestedName, client, !*noBrowser)
	default:
		fmt.Fprintln(os.Stderr, "error: --auth-flow must be one of: device, browser")
		return 2
	}
	account.Provider = provider.ID
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	store, err := loadAccountsOrEmpty(*accountsPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	idx := findMatchingAccount(store.Accounts, account)
	if idx >= 0 {
		upsert, reason, existing, candidate := shouldUpsertMatchedAccount(store.Accounts[idx], account, requestedName, client, recheckAccountPlanType)
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
			if err := saveAccounts(*accountsPath, store); err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				return 1
			}
			fmt.Printf("Updated account %q (%s).\n", account.Name, account.ID)
			return 0
		}
	}

	store.Accounts = append(store.Accounts, account)
	if err := saveAccounts(*accountsPath, store); err != nil {
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
	accountsPath := fs.String("accounts-file", defaultAccountsPath(), "path to accounts.json")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: accounts remove [--accounts-file path] <id-or-name>")
		return 2
	}
	target := strings.TrimSpace(fs.Arg(0))

	store, err := loadAccountsOrEmpty(*accountsPath)
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

	if err := saveAccounts(*accountsPath, store); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if err := removeAccountUsageCache(removed.ID); err != nil {
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
	accountsPath := fs.String("accounts-file", defaultAccountsPath(), "path to accounts.json")
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

	store, err := loadAccountsOrEmpty(*accountsPath)
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
	if err := saveAccounts(*accountsPath, store); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	fmt.Printf("Renamed account %q (%s) to %q.\n", oldName, store.Accounts[index].ID, newName)
	return 0
}

func printAccountsList(accounts []storedAccount) {
	if len(accounts) == 0 {
		fmt.Println("No accounts found.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tPROVIDER\tEMAIL\tPLAN\tAUTH TYPE")
	for _, acc := range accounts {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", acc.ID, acc.Name, acc.Provider, valueOrDash(acc.Email), accountPlan(acc), acc.AuthData.Type)
	}
	_ = w.Flush()
}

func findMatchingAccount(accounts []storedAccount, candidate storedAccount) int {
	if candidate.Email != nil {
		email := strings.ToLower(strings.TrimSpace(*candidate.Email))
		if email != "" {
			for i := range accounts {
				if accounts[i].Provider == candidate.Provider && accounts[i].Email != nil && strings.EqualFold(strings.TrimSpace(*accounts[i].Email), email) {
					return i
				}
			}
		}
	}

	return -1
}

type planRecheckFunc func(storedAccount, *http.Client) (storedAccount, string, bool)

func shouldUpsertMatchedAccount(existing, candidate storedAccount, requestedName string, client *http.Client, recheckPlan planRecheckFunc) (bool, string, storedAccount, storedAccount) {
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

func recheckAccountPlanType(account storedAccount, client *http.Client) (storedAccount, string, bool) {
	if client == nil {
		return account, "", false
	}

	refreshed, _, err := ensureFreshTokens(account, client)
	if err != nil {
		return account, "", false
	}
	account = refreshed

	usage, err := fetchUsage(account, client)
	if err != nil {
		return account, "", false
	}

	plan := strings.TrimSpace(usage.PlanType)
	if plan == "" {
		return account, "", false
	}

	account.PlanType = strPtr(plan)
	return account, strings.ToLower(plan), true
}

func findAccountForRemoval(accounts []storedAccount, target string) (int, error) {
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
	fmt.Println("  check-usage accounts add [--accounts-file path] --provider opencode-go|deepseek (--api-key key|--api-key-env name) [--name name]")
	fmt.Println("  check-usage accounts login [--accounts-file path] --provider codex [--name name] [--timeout seconds] [--no-browser] [--auth-flow device|browser]")
	fmt.Println("  check-usage accounts reauth [--accounts-file path] [--api-key key|--api-key-env name] [--timeout seconds] [--no-browser] [--auth-flow device|browser] <id-or-name>")
	fmt.Println("  check-usage accounts remove [--accounts-file path] <id-or-name>")
	fmt.Println("  check-usage accounts rename [--accounts-file path] <id-or-name> <new-name>")
}

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
	case "login":
		return runAccountsLogin(args[1:])
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

func runAccountsList(args []string) int {
	fs := flag.NewFlagSet("accounts list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
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
	accountsPath := fs.String("accounts-file", defaultAccountsPath(), "path to accounts.json")
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

	client := &http.Client{Timeout: time.Duration(*timeout) * time.Second}
	flow := strings.ToLower(strings.TrimSpace(*authFlow))
	var account storedAccount
	var err error
	switch flow {
	case "device":
		account, err = runDeviceAuthLogin(strings.TrimSpace(*name), client, !*noBrowser)
	case "browser", "oauth":
		account, err = runOAuthLogin(strings.TrimSpace(*name), client, !*noBrowser)
	default:
		fmt.Fprintln(os.Stderr, "error: --auth-flow must be one of: device, browser")
		return 2
	}
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
		account.ID = store.Accounts[idx].ID
		if strings.TrimSpace(*name) == "" && strings.TrimSpace(store.Accounts[idx].Name) != "" {
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

	fmt.Printf("Removed account %q (%s).\n", removed.Name, removed.ID)
	return 0
}

func runAccountsRename(args []string) int {
	fs := flag.NewFlagSet("accounts rename", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
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
	fmt.Fprintln(w, "ID\tNAME\tEMAIL\tPLAN\tAUTH TYPE")
	for _, acc := range accounts {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", acc.ID, acc.Name, valueOrDash(acc.Email), valueOrDash(acc.PlanType), acc.AuthData.Type)
	}
	_ = w.Flush()
}

func findMatchingAccount(accounts []storedAccount, candidate storedAccount) int {
	if candidate.AuthData.AccountID != nil && strings.TrimSpace(*candidate.AuthData.AccountID) != "" {
		for i := range accounts {
			if accounts[i].AuthData.AccountID != nil && *accounts[i].AuthData.AccountID == *candidate.AuthData.AccountID {
				return i
			}
		}
	}

	if candidate.Email != nil {
		email := strings.ToLower(strings.TrimSpace(*candidate.Email))
		if email != "" {
			for i := range accounts {
				if accounts[i].Email != nil && strings.EqualFold(strings.TrimSpace(*accounts[i].Email), email) {
					return i
				}
			}
		}
	}

	return -1
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
	fmt.Println("  codex-usage accounts list [--accounts-file path]")
	fmt.Println("  codex-usage accounts login [--accounts-file path] [--name name] [--timeout seconds] [--no-browser] [--auth-flow device|browser]")
	fmt.Println("  codex-usage accounts remove [--accounts-file path] <id-or-name>")
	fmt.Println("  codex-usage accounts rename [--accounts-file path] <id-or-name> <new-name>")
}

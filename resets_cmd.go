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

func runResetsCommand(args []string) int {
	fs := flag.NewFlagSet("resets", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	accountsPath := fs.String("accounts-file", defaultAccountsPath(), "path to accounts.json")
	timeout := fs.Int("timeout", 20, "HTTP timeout in seconds")
	showUsed := fs.Bool("show-used", false, "include redeemed, expired, and other unavailable reset credits")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: codex-usage resets [--accounts-file path] [--timeout seconds] [--show-used] <account name/email/id>")
		return 2
	}
	target := strings.TrimSpace(fs.Arg(0))

	store, err := loadAccounts(*accountsPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	idx, err := findAccountByIDNameOrEmail(store.Accounts, target)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	account := store.Accounts[idx]
	if normalizeAuthType(account.AuthData.Type) != "chatgpt" {
		fmt.Fprintf(os.Stderr, "error: account %q uses auth type %q; reset credits require a ChatGPT account\n", account.Name, account.AuthData.Type)
		return 1
	}

	client := &http.Client{Timeout: time.Duration(*timeout) * time.Second}
	updated, changed, err := ensureFreshTokens(account, client)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if changed {
		store.Accounts[idx] = updated
		if err := saveAccounts(*accountsPath, store); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
	}

	credits, err := fetchResetCredits(updated, client)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	printResetCreditsDetails(updated, credits, *showUsed, time.Now())
	return 0
}

func findAccountByIDNameOrEmail(accounts []storedAccount, target string) (int, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return -1, fmt.Errorf("account target cannot be empty")
	}

	matches := make([]int, 0, 1)
	for i := range accounts {
		if accounts[i].ID == target || strings.EqualFold(strings.TrimSpace(accounts[i].Name), target) || accountEmailMatches(accounts[i], target) {
			matches = append(matches, i)
		}
	}

	switch len(matches) {
	case 0:
		return -1, fmt.Errorf("account not found: %s", target)
	case 1:
		return matches[0], nil
	default:
		return -1, fmt.Errorf("multiple accounts match %q; use account ID instead", target)
	}
}

func accountEmailMatches(account storedAccount, target string) bool {
	return account.Email != nil && strings.EqualFold(strings.TrimSpace(*account.Email), target)
}

func printResetCreditsDetails(account storedAccount, payload *resetCreditsPayload, showUsed bool, now time.Time) {
	fmt.Printf("Account: %s\n", account.Name)
	fmt.Printf("Email: %s\n", valueOrDash(account.Email))
	fmt.Printf("Available reset credits: %d\n", payload.AvailableCount)
	fmt.Printf("Total earned reset credits: %d\n", payload.TotalEarnedCount)

	credits := filteredResetCredits(payload.Credits, showUsed)
	if len(credits) == 0 {
		if showUsed {
			fmt.Println("No reset credits found.")
		} else {
			fmt.Println("No available reset credits found. Use --show-used to include redeemed or expired credits.")
		}
		return
	}

	sortResetCredits(credits)
	fmt.Println()
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "#\tSTATUS\tTITLE\tGAINED\tEXPIRES\tREMAINING\tREDEEM STARTED\tREDEEMED")
	for i, credit := range credits {
		fmt.Fprintf(
			w,
			"%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			i+1,
			valueOrUnknown(credit.Status),
			valueOrDashString(credit.Title),
			resetCreditTimeText(credit.GrantedAt, now, false),
			resetCreditTimeText(credit.ExpiresAt, now, false),
			resetCreditRemainingText(credit.ExpiresAt, now),
			resetCreditTimeText(credit.RedeemStartedAt, now, false),
			resetCreditTimeText(credit.RedeemedAt, now, false),
		)
	}
	_ = w.Flush()
}

func filteredResetCredits(credits []resetCreditDetail, showUsed bool) []resetCreditDetail {
	filtered := make([]resetCreditDetail, 0, len(credits))
	for _, credit := range credits {
		if showUsed || resetCreditAvailable(credit) {
			filtered = append(filtered, credit)
		}
	}
	return filtered
}

func resetCreditAvailable(credit resetCreditDetail) bool {
	return strings.EqualFold(strings.TrimSpace(credit.Status), "available")
}

func valueOrUnknown(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}
	return value
}

func valueOrDashString(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

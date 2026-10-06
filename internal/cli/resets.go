package cli

import (
	"bytes"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/McMelonTV/check-usage/internal/codexapi"
	"github.com/McMelonTV/check-usage/internal/storage"
	"github.com/McMelonTV/check-usage/internal/usage"
)

func runResetsCommand(args []string) int {
	fs := flag.NewFlagSet("resets", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	setDoubleDashFlagUsage(fs)
	accountsPath := fs.String("accounts-file", storage.DefaultAccountsPath(), "path to accounts.json")
	timeout := fs.Int("timeout", 20, "HTTP timeout in seconds")
	showUsed := fs.Bool("show-used", false, "include redeemed, expired, and other unavailable reset credits")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: check-usage resets [--accounts-file path] [--timeout seconds] [--show-used] <account name/email/id>")
		return 2
	}
	target := strings.TrimSpace(fs.Arg(0))

	store, err := storage.LoadAccounts(*accountsPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	idx, err := storage.FindAccount(store.Accounts, target)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	account := store.Accounts[idx]
	provider, err := usage.ProviderFor(account.Provider)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if !provider.ResetCredits {
		fmt.Fprintf(os.Stderr, "error: reset credits are unavailable for %s accounts\n", provider.Name)
		return 1
	}

	client := &http.Client{Timeout: time.Duration(*timeout) * time.Second}
	updated, changed, err := usage.EnsureFreshTokens(account, client)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if changed {
		store.Accounts[idx] = updated
		if err := storage.SaveAccounts(*accountsPath, store); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
	}

	credits, err := usage.FetchResetCredits(updated, client)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if err := storage.UpdateResetCache(updated.ID, credits, time.Now().Unix()); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	printResetCreditsDetails(updated, credits, *showUsed, time.Now())
	return 0
}

func printResetCreditsDetails(account storage.Account, payload *codexapi.ResetCreditsPayload, showUsed bool, now time.Time) {
	fmt.Printf("%s %s\n", usage.HeaderText("Account:"), account.Name)
	fmt.Printf("%s %s\n", usage.HeaderText("Email:"), usage.ValueOrDash(account.Email))
	fmt.Printf("%s %s\n", usage.HeaderText("Available reset credits:"), usage.ColorizeAvailableResetCreditCount(payload.AvailableCount))
	fmt.Printf("%s %d\n", usage.HeaderText("Total earned reset credits:"), payload.TotalEarnedCount)

	credits := usage.FilteredResetCredits(payload.Credits, showUsed)
	if len(credits) == 0 {
		fmt.Println()
		if showUsed {
			fmt.Println("No reset credits found.")
		} else {
			fmt.Println("No available reset credits found. Use --show-used to include redeemed or expired credits.")
		}
		return
	}

	usage.SortResetCredits(credits)
	fmt.Println()
	var b bytes.Buffer
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "#\tSTATUS\tTITLE\tGAINED\tEXPIRES\tREMAINING\tREDEEM STARTED\tREDEEMED")
	for i, credit := range credits {
		fmt.Fprintf(
			w,
			"%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			i+1,
			usage.ValueOrUnknown(credit.Status),
			usage.ValueOrDashString(credit.Title),
			usage.ResetCreditTimeText(credit.GrantedAt, now, false),
			usage.ResetCreditTimeText(credit.ExpiresAt, now, false),
			usage.ResetCreditRemainingText(credit.ExpiresAt, now),
			usage.ResetCreditTimeText(credit.RedeemStartedAt, now, false),
			usage.ResetCreditTimeText(credit.RedeemedAt, now, false),
		)
	}
	_ = w.Flush()
	fmt.Print(usage.ColorizeTableOutput(usage.ApplyResetCreditStatusColors(b.String(), credits)))
}

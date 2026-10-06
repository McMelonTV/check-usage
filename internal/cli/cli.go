// Package cli implements the check-usage command line: the root usage command and the accounts, resets, and api subcommands.
package cli

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/McMelonTV/check-usage/internal/storage"
	"github.com/McMelonTV/check-usage/internal/terminal"
	"github.com/McMelonTV/check-usage/internal/tui"
	"github.com/McMelonTV/check-usage/internal/usage"
	"github.com/charmbracelet/x/term"
)

// Run executes the check-usage command line and returns the process exit code.
func Run(args []string) int {
	terminal.ConfigureANSIOutput()

	if len(args) > 1 {
		switch args[1] {
		case "api":
			return runAPICommand(args[2:])
		case "accounts":
			return runAccountsCommand(args[2:])
		case "resets":
			return runResetsCommand(args[2:])
		}
	}

	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	accountsPath := fs.String("accounts-file", storage.DefaultAccountsPath(), "path to accounts.json")
	timeout := fs.Int("timeout", 20, "HTTP timeout in seconds")
	plain := fs.Bool("plain", false, "print the non-interactive usage table")
	fs.Usage = func() { printRootCommandUsage(fs) }
	if err := fs.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}

	client := &http.Client{Timeout: time.Duration(*timeout) * time.Second}
	if !*plain && term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd()) {
		if err := tui.Run(*accountsPath, client); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		return 0
	}

	defer fmt.Print(usage.ANSIReset)
	rows, err := usage.CollectRows(*accountsPath, client)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	usage.PrintTable(rows)
	return 0
}

func printRootCommandUsage(fs *flag.FlagSet) {
	fmt.Println(usage.HeaderText("Usage:"))
	fmt.Printf("  %s [flags]\n", os.Args[0])
	fmt.Printf("  %s accounts <command> [flags]\n", os.Args[0])
	fmt.Printf("  %s resets [flags] <account name/email/id>\n", os.Args[0])
	fmt.Printf("  %s api [flags] <method|serve> [params-json|-]\n", os.Args[0])
	fmt.Println()
	fmt.Println(usage.HeaderText("Subcommands:"))
	fmt.Println("  accounts  manage saved accounts")
	fmt.Println("  resets    show reset-credit details for one account")
	fmt.Println("  api       JSON-RPC interface for applications and scripts")
	fmt.Println()
	fmt.Println(usage.HeaderText("Flags:"))
	printDoubleDashFlagDefaults(fs)
}

package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/charmbracelet/x/term"
)

func main() {
	configureANSIOutput()

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "accounts":
			os.Exit(runAccountsCommand(os.Args[2:]))
		case "resets":
			os.Exit(runResetsCommand(os.Args[2:]))
		}
	}

	fs := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	accountsPath := fs.String("accounts-file", defaultAccountsPath(), "path to accounts.json")
	timeout := fs.Int("timeout", 20, "HTTP timeout in seconds")
	plain := fs.Bool("plain", false, "print the non-interactive usage table")
	fs.Usage = func() { printRootCommandUsage(fs) }
	if err := fs.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			return
		}
		os.Exit(2)
	}

	client := &http.Client{Timeout: time.Duration(*timeout) * time.Second}
	if !*plain && term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd()) {
		if err := runTUI(*accountsPath, client); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	defer fmt.Print(ansiReset)
	rows, err := collectUsageRows(*accountsPath, client)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	printTable(rows)
}

func printRootCommandUsage(fs *flag.FlagSet) {
	fmt.Println(headerText("Usage:"))
	fmt.Printf("  %s [flags]\n", os.Args[0])
	fmt.Printf("  %s accounts <command> [flags]\n", os.Args[0])
	fmt.Printf("  %s resets [flags] <account name/email/id>\n", os.Args[0])
	fmt.Println()
	fmt.Println(headerText("Subcommands:"))
	fmt.Println("  accounts  manage saved accounts")
	fmt.Println("  resets    show reset-credit details for one account")
	fmt.Println()
	fmt.Println(headerText("Flags:"))
	printDoubleDashFlagDefaults(fs)
}

package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"
)

func main() {
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
	showColorConfig := fs.Bool("show-color-config", false, "print usage color thresholds and exit")
	fs.Usage = func() { printRootCommandUsage(fs) }
	if err := fs.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			return
		}
		os.Exit(2)
	}

	defer fmt.Print(ansiReset)

	if *showColorConfig {
		printColorConfig()
		return
	}

	client := &http.Client{Timeout: time.Duration(*timeout) * time.Second}
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
	fs.PrintDefaults()
}

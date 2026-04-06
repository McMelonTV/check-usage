package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "accounts" {
		os.Exit(runAccountsCommand(os.Args[2:]))
	}

	defer fmt.Print(ansiReset)

	accountsPath := flag.String("accounts-file", defaultAccountsPath(), "path to accounts.json")
	timeout := flag.Int("timeout", 20, "HTTP timeout in seconds")
	showColorConfig := flag.Bool("show-color-config", false, "print usage color thresholds and exit")
	flag.Parse()

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

// Package cli implements the usage server and its paired terminal client.
package cli

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/McMelonTV/check-usage/internal/remote"
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
		case "server":
			return runServerCommand(args[2:])
		case "connect":
			return runConnectCommand(args[2:])
		case "api":
			return runRemoteAPICommand(args[2:])
		case "accounts":
			return runRemoteAccountsCommand(args[2:])
		case "resets":
			return runRemoteResetsCommand(args[2:])
		case "update":
			return runUpdateCommand(args[2:])
		case "version":
			return runVersionCommand(args[2:])
		}
	}

	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	connectionPath := fs.String("connection-file", remote.DefaultConfigPath(), "device server connection file")
	timeout := fs.Int("timeout", 120, "server request timeout in seconds")
	plain := fs.Bool("plain", false, "print the non-interactive usage table")
	showVersion := fs.Bool("version", false, "print the installed version and exit")
	fs.Usage = func() { printRootCommandUsage(fs) }
	if err := fs.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return 2
	}
	if *showVersion {
		fmt.Println(FormatVersion())
		return 0
	}

	interactiveTTY := !*plain && term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd())
	config, err := loadClientConfig(*connectionPath)
	if err != nil {
		if interactiveTTY {
			if _, statErr := os.Stat(*connectionPath); os.IsNotExist(statErr) {
				fmt.Fprintln(os.Stderr, "Not connected to a usage server yet.")
				fmt.Fprintln(os.Stderr, "Run `check-usage server pair` on your homelab (or `check-usage server setup` to set up a server here).")
				fmt.Fprintln(os.Stderr)
				config, err = connectFromReader(*connectionPath, "", os.Stdin, true)
			}
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
	}
	client, err := remote.New(config, &http.Client{Timeout: time.Duration(*timeout) * time.Second})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if interactiveTTY {
		if err := tui.RunRemote(*connectionPath, client); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		return 0
	}

	defer fmt.Print(usage.ANSIReset)
	results, err := client.Usage(context.Background(), "", true)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	rows, _ := remote.Rows(results)
	usage.PrintTable(rows)
	return 0
}

func printRootCommandUsage(fs *flag.FlagSet) {
	prog := fs.Name()
	if prog == "" {
		prog = os.Args[0]
	}
	fmt.Println(usage.HeaderText("Usage:"))
	fmt.Printf("  %s [flags]\n", prog)
	fmt.Printf("  %s connect [--url <server HTTPS origin>] [flags]\n", prog)
	fmt.Printf("  %s server [--config path] <setup|serve|pair|devices|accounts|service|init|api> [flags]\n", prog)
	fmt.Printf("  %s accounts [flags] <list|rename|remove> [arguments]\n", prog)
	fmt.Printf("  %s resets [flags] <account name/email/id>\n", prog)
	fmt.Printf("  %s api [flags] <method> [params-json|-]\n", prog)
	fmt.Printf("  %s update [--check] [--version tag] [--force]\n", prog)
	fmt.Printf("  %s version\n", prog)
	fmt.Println()
	fmt.Println(usage.HeaderText("Subcommands:"))
	fmt.Println("  connect   pair this device with a usage server (accepts a pairing link or URL + code)")
	fmt.Println("  server    run the setup wizard, serve usage, pair devices, or administer accounts locally")
	fmt.Println("  accounts  list, rename, or remove shared server accounts")
	fmt.Println("  resets    show reset-credit details for one account")
	fmt.Println("  api       call the authenticated server JSON-RPC interface")
	fmt.Println("  update    update check-usage (and its cu alias) from GitHub Releases")
	fmt.Println("  version   print the installed check-usage version")
	fmt.Println()
	fmt.Println(usage.HeaderText("Flags:"))
	printDoubleDashFlagDefaults(fs)
}

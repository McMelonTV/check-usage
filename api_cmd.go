package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/McMelonTV/check-usage/usageapi"
)

func runAPICommand(args []string) int {
	fs := flag.NewFlagSet("api", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	setDoubleDashFlagUsage(fs)
	accountsPath := fs.String("accounts-file", defaultAccountsPath(), "path to accounts.json")
	cacheDir := fs.String("cache-dir", "", "directory for per-account usage snapshots")
	timeout := fs.Int("timeout", 30, "HTTP timeout in seconds")
	pretty := fs.Bool("pretty", false, "indent one-shot JSON output")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if fs.NArg() < 1 || fs.NArg() > 2 {
		fmt.Fprintln(os.Stderr, "usage: check-usage api [flags] <method|serve> [params-json|-]")
		return 2
	}

	service := usageapi.New(usageapi.Config{
		AccountsFile: *accountsPath,
		CacheDir:     *cacheDir,
		HTTPClient:   &http.Client{Timeout: time.Duration(*timeout) * time.Second},
		UserAgent:    userAgent,
	})
	server := usageapi.RPCServer{Service: service}
	if fs.Arg(0) == "serve" {
		if fs.NArg() != 1 {
			fmt.Fprintln(os.Stderr, "api serve does not accept one-shot params")
			return 2
		}
		if err := server.Serve(context.Background(), os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		return 0
	}

	params := json.RawMessage("{}")
	if fs.NArg() == 2 {
		value := fs.Arg(1)
		if value == "-" {
			content, err := io.ReadAll(os.Stdin)
			if err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				return 1
			}
			value = string(content)
		}
		if strings.TrimSpace(value) == "" || !json.Valid([]byte(value)) {
			fmt.Fprintln(os.Stderr, "params must be one valid JSON value")
			return 2
		}
		params = json.RawMessage(value)
	}

	response := server.Handle(context.Background(), usageapi.RPCRequest{
		JSONRPC: "2.0", ID: json.RawMessage("1"), Method: fs.Arg(0), Params: params,
	})
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	if *pretty {
		encoder.SetIndent("", "  ")
	}
	if err := encoder.Encode(response); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if response.Error != nil {
		return 1
	}
	return 0
}

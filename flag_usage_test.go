package main

import (
	"bytes"
	"flag"
	"strings"
	"testing"
)

func TestFlagUsageUsesDoubleDashes(t *testing.T) {
	var output bytes.Buffer
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(&output)
	setDoubleDashFlagUsage(fs)
	fs.String("accounts-file", "", "path to accounts.json")
	fs.Bool("show-used", false, "include unavailable reset credits")

	err := fs.Parse([]string{"--help"})
	if err != flag.ErrHelp {
		t.Fatalf("Parse(--help) error = %v, want flag.ErrHelp", err)
	}

	got := output.String()
	for _, want := range []string{"  --accounts-file string", "  --show-used"} {
		if !strings.Contains(got, want) {
			t.Errorf("help output does not contain %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"  -accounts-file string", "  -show-used"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("help output contains single-dash flag %q:\n%s", unwanted, got)
		}
	}
}

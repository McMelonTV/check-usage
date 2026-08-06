package main

import (
	"bytes"
	"flag"
	"fmt"
	"strings"
)

func setDoubleDashFlagUsage(fs *flag.FlagSet) {
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage of %s:\n", fs.Name())
		printDoubleDashFlagDefaults(fs)
	}
}

func printDoubleDashFlagDefaults(fs *flag.FlagSet) {
	output := fs.Output()

	var defaults bytes.Buffer
	fs.SetOutput(&defaults)
	fs.PrintDefaults()
	fs.SetOutput(output)

	lines := strings.SplitAfter(defaults.String(), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "  -") {
			line = "  --" + strings.TrimPrefix(line, "  -")
		}
		fmt.Fprint(output, line)
	}
}

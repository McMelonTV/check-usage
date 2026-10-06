package main

import (
	"os"

	"github.com/McMelonTV/check-usage/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args))
}

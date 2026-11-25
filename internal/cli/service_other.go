//go:build !linux

package cli

import (
	"fmt"
	"os"
)

func systemdAvailable() bool {
	return false
}

func installSystemdService(string) (string, error) {
	return "", fmt.Errorf("systemd user service installation is only supported on Linux")
}

func runServerServiceCommand(string, []string) int {
	fmt.Fprintln(os.Stderr, "error: server service management is only supported on Linux with systemd")
	return 1
}

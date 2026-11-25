//go:build linux

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/McMelonTV/check-usage/internal/remoteserver"
)

const systemdUnitName = "check-usage.service"

func systemdAvailable() bool {
	_, err := exec.LookPath("systemctl")
	return err == nil
}

func systemdUnitPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "systemd", "user", systemdUnitName), nil
}

func systemdQuote(value string) string {
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	escaped = strings.ReplaceAll(escaped, `%`, `%%`)
	return `"` + escaped + `"`
}

func systemdUnitContent(binaryPath, configPath, pathEnv string) string {
	execStart := systemdQuote(binaryPath) + " server"
	if configPath != "" && configPath != remoteserver.DefaultConfigPath() {
		execStart += " --config " + systemdQuote(configPath)
	}
	execStart += " serve"
	var b strings.Builder
	b.WriteString("[Unit]\n")
	b.WriteString("Description=Shared AI usage server\n\n")
	b.WriteString("[Service]\n")
	b.WriteString("ExecStart=" + execStart + "\n")
	if strings.TrimSpace(pathEnv) != "" {
		b.WriteString("Environment=" + systemdQuote("PATH="+pathEnv) + "\n")
	}
	b.WriteString("Restart=on-failure\n")
	b.WriteString("RestartSec=5\n\n")
	b.WriteString("[Install]\n")
	b.WriteString("WantedBy=default.target\n")
	return b.String()
}

func resolveServiceBinaryPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err == nil {
		return resolved, nil
	}
	return filepath.Abs(exe)
}

func installSystemdService(configPath string) (string, error) {
	if !systemdAvailable() {
		return "", fmt.Errorf("systemctl is not installed on this host")
	}
	binaryPath, err := resolveServiceBinaryPath()
	if err != nil {
		return "", fmt.Errorf("resolve check-usage binary path: %w", err)
	}
	unitPath, err := systemdUnitPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(unitPath), 0o755); err != nil {
		return "", err
	}
	content := systemdUnitContent(binaryPath, configPath, os.Getenv("PATH"))
	if err := os.WriteFile(unitPath, []byte(content), 0o644); err != nil {
		return "", err
	}
	if out, err := exec.Command("systemctl", "--user", "daemon-reload").CombinedOutput(); err != nil {
		return unitPath, fmt.Errorf("systemctl --user daemon-reload failed: %s", strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("systemctl", "--user", "enable", "--now", systemdUnitName).CombinedOutput(); err != nil {
		return unitPath, fmt.Errorf("systemctl --user enable --now %s failed: %s", systemdUnitName, strings.TrimSpace(string(out)))
	}
	return unitPath, nil
}

func uninstallSystemdService() error {
	if !systemdAvailable() {
		return fmt.Errorf("systemctl is not installed on this host")
	}
	_ = exec.Command("systemctl", "--user", "disable", "--now", systemdUnitName).Run()
	unitPath, err := systemdUnitPath()
	if err != nil {
		return err
	}
	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	return nil
}

func restartSystemdServiceIfActive() (bool, error) {
	if !systemdAvailable() {
		return false, nil
	}
	unitPath, err := systemdUnitPath()
	if err != nil {
		return false, nil
	}
	if _, err := os.Stat(unitPath); err != nil {
		return false, nil
	}
	if err := exec.Command("systemctl", "--user", "is-active", "--quiet", systemdUnitName).Run(); err != nil {
		return false, nil
	}
	if out, err := exec.Command("systemctl", "--user", "restart", systemdUnitName).CombinedOutput(); err != nil {
		return true, fmt.Errorf("systemctl --user restart %s failed: %s", systemdUnitName, strings.TrimSpace(string(out)))
	}
	return true, nil
}

func runServerServiceCommand(configPath string, args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: check-usage server service install|status|uninstall")
		return 2
	}
	switch args[0] {
	case "install":
		unitPath, err := installSystemdService(configPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		fmt.Println("Installed and started systemd user service:", unitPath)
		fmt.Println("View logs with: journalctl --user -u check-usage.service -f")
		fmt.Println("Tip: enable user lingering if you want the service to start at boot without logging in.")
		return 0
	case "status":
		if !systemdAvailable() {
			fmt.Fprintln(os.Stderr, "error: systemctl is not installed on this host")
			return 1
		}
		cmd := exec.Command("systemctl", "--user", "status", "--no-pager", systemdUnitName)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return 1
		}
		return 0
	case "uninstall":
		if err := uninstallSystemdService(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		fmt.Println("Stopped and removed systemd user service.")
		return 0
	default:
		fmt.Fprintln(os.Stderr, "usage: check-usage server service install|status|uninstall")
		return 2
	}
}

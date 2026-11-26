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

// lingerEnabled reports whether systemd keeps this user's services running
// without an active login session. Unknown (no loginctl) counts as enabled so
// hosts without logind are not warned about.
func lingerEnabled(user string) bool {
	if _, err := exec.LookPath("loginctl"); err != nil {
		return true
	}
	out, err := exec.Command("loginctl", "show-user", user, "--property=Linger", "--value").Output()
	if err != nil {
		// show-user fails when the user has no session and lingering is off.
		return false
	}
	return strings.TrimSpace(string(out)) == "yes"
}

// ensureLinger enables lingering for the current user. Without it, systemd
// stops the user service when the last SSH session closes, so remote clients
// can only reach the server while someone is logged in to it.
// It returns a user-facing warning when lingering stays off.
func ensureLinger() string {
	user := currentUserName()
	if user == "" || lingerEnabled(user) {
		return ""
	}
	if err := exec.Command("loginctl", "enable-linger", user).Run(); err == nil && lingerEnabled(user) {
		fmt.Println("  Enabled user lingering so the server keeps running after you log out.")
		return ""
	}
	return fmt.Sprintf("user lingering is off, so systemd stops the server when you log out of this host.\n  Fix: sudo loginctl enable-linger %s", user)
}

func currentUserName() string {
	if name := strings.TrimSpace(os.Getenv("USER")); name != "" {
		return name
	}
	out, err := exec.Command("id", "-un").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
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
	// Existing installs predate automatic lingering; fix them on update.
	if warning := ensureLinger(); warning != "" {
		fmt.Fprintln(os.Stderr, "warning:", warning)
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
		if warning := ensureLinger(); warning != "" {
			fmt.Fprintln(os.Stderr, "warning:", warning)
		}
		return 0
	case "status":
		if !systemdAvailable() {
			fmt.Fprintln(os.Stderr, "error: systemctl is not installed on this host")
			return 1
		}
		if user := currentUserName(); user != "" && !lingerEnabled(user) {
			fmt.Fprintf(os.Stderr, "warning: user lingering is off, so systemd stops the server when you log out of this host.\n  Fix: sudo loginctl enable-linger %s\n\n", user)
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

//go:build linux

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
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
	// The server exits cleanly when this file changes, and Restart=always
	// starts the new binary, so upgrades take effect without a manual restart.
	b.WriteString("Environment=" + systemdQuote(serviceBinaryEnv+"="+binaryPath) + "\n")
	b.WriteString("Restart=always\n")
	b.WriteString("RestartSec=5\n\n")
	b.WriteString("[Install]\n")
	b.WriteString("WantedBy=default.target\n")
	return b.String()
}

func resolveServiceBinaryPath() (string, error) {
	exe, err := resolveSelfExecutablePath()
	if err != nil {
		return "", err
	}
	return stableServiceBinaryPath(exe), nil
}

var (
	unitConfigPattern = regexp.MustCompile(`--config "((?:[^"\\]|\\.)*)"`)
	unitPathPattern   = regexp.MustCompile(`(?m)^Environment="PATH=((?:[^"\\]|\\.)*)"$`)
)

func systemdUnquote(value string) string {
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] == '\\' && i+1 < len(value) {
			i++
		}
		b.WriteByte(value[i])
	}
	return strings.ReplaceAll(b.String(), "%%", "%")
}

// existingUnitSettings recovers the config path and PATH from a unit this
// command wrote earlier, so rewriting it keeps the user's choices.
func existingUnitSettings(unit string) (configPath, pathEnv string) {
	if match := unitConfigPattern.FindStringSubmatch(unit); match != nil {
		configPath = systemdUnquote(match[1])
	}
	if match := unitPathPattern.FindStringSubmatch(unit); match != nil {
		pathEnv = systemdUnquote(match[1])
	}
	return configPath, pathEnv
}

// serviceRunsBinary reports whether the service's main process runs binaryPath.
// Unknown counts as yes, so an unreadable state never causes a restart.
func serviceRunsBinary(binaryPath string) bool {
	out, err := exec.Command("systemctl", "--user", "show", "--property=MainPID", "--value", systemdUnitName).Output()
	if err != nil {
		return true
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || pid <= 0 {
		return true
	}
	running, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid), "exe"))
	if err != nil {
		return true
	}
	wanted, err := os.Stat(binaryPath)
	if err != nil {
		return true
	}
	return os.SameFile(running, wanted)
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

// syncSystemdService moves an installed service onto binaryPath. It rewrites
// units from older releases (versioned mise paths, no upgrade watch), and
// restarts an active service when its unit changed or it still runs an older
// binary. A stopped service stays stopped.
func syncSystemdService(binaryPath string) (bool, error) {
	if !systemdAvailable() {
		return false, nil
	}
	unitPath, err := systemdUnitPath()
	if err != nil {
		return false, nil
	}
	current, err := os.ReadFile(unitPath)
	if err != nil {
		return false, nil
	}
	configPath, pathEnv := existingUnitSettings(string(current))
	changed := false
	if wanted := systemdUnitContent(binaryPath, configPath, pathEnv); wanted != string(current) {
		if err := os.WriteFile(unitPath, []byte(wanted), 0o644); err != nil {
			return false, fmt.Errorf("update %s: %w", unitPath, err)
		}
		if out, err := exec.Command("systemctl", "--user", "daemon-reload").CombinedOutput(); err != nil {
			return false, fmt.Errorf("systemctl --user daemon-reload failed: %s", strings.TrimSpace(string(out)))
		}
		changed = true
	}
	if err := exec.Command("systemctl", "--user", "is-active", "--quiet", systemdUnitName).Run(); err != nil {
		return false, nil
	}
	if !changed && serviceRunsBinary(binaryPath) {
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

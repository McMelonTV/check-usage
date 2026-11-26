package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// miseInstall describes a binary that lives in a mise install directory:
// <data>/installs/<tool-dir>/<version>/<rel>.
type miseInstall struct {
	ToolDir string // <data>/installs/<tool-dir>
	Rel     string // path of the binary inside the version directory
}

func parseMiseInstall(exePath string) (miseInstall, bool) {
	parts := strings.Split(filepath.ToSlash(filepath.Clean(exePath)), "/")
	// Need <data>/installs/<tool>/<version>/<at least one path element>, where
	// <data> is named mise (the default) or the tool directory carries mise's
	// backend metadata (a custom MISE_DATA_DIR).
	for i := 1; i+3 < len(parts); i++ {
		if !strings.EqualFold(parts[i], "installs") {
			continue
		}
		install := miseInstall{
			ToolDir: filepath.FromSlash(strings.Join(parts[:i+2], "/")),
			Rel:     filepath.FromSlash(strings.Join(parts[i+3:], "/")),
		}
		if strings.EqualFold(parts[i-1], "mise") {
			return install, true
		}
		if _, err := os.Stat(filepath.Join(install.ToolDir, ".mise.backend.toml")); err == nil {
			return install, true
		}
	}
	return miseInstall{}, false
}

func isMiseManagedPath(exePath string) bool {
	_, ok := parseMiseInstall(exePath)
	return ok
}

// stableServiceBinaryPath returns a path that keeps working across upgrades.
// For mise installs that is <tool-dir>/latest/<rel>, which mise repoints to
// the newest installed version, so pruning old versions cannot break the
// service. It is only used while it currently resolves to exePath, so a user
// who pinned an older version keeps running that version.
func stableServiceBinaryPath(exePath string) string {
	install, ok := parseMiseInstall(exePath)
	if !ok {
		return exePath
	}
	candidate := filepath.Join(install.ToolDir, "latest", install.Rel)
	current, err := os.Stat(exePath)
	if err != nil {
		return exePath
	}
	if latest, err := os.Stat(candidate); err == nil && os.SameFile(current, latest) {
		return candidate
	}
	return exePath
}

// miseToolName reads the tool request (for example github:McMelonTV/check-usage)
// that mise recorded when installing exePath.
func miseToolName(exePath, fallback string) string {
	install, ok := parseMiseInstall(exePath)
	if !ok {
		return fallback
	}
	file, err := os.Open(filepath.Join(install.ToolDir, ".mise.backend.toml"))
	if err != nil {
		return fallback
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key, value, found := strings.Cut(scanner.Text(), "=")
		if found && strings.TrimSpace(key) == "full" {
			if name := strings.Trim(strings.TrimSpace(value), `"`); name != "" {
				return name
			}
		}
	}
	return fallback
}

// miseUpgradeArgs upgrades to the newest release and records the request as
// `latest`, so later plain `mise upgrade` runs keep following new releases
// instead of staying on a pinned major version. mise's minimum release age
// applies unless ignoreAge is set, and then only for this one command.
func miseUpgradeArgs(tool string, ignoreAge bool) []string {
	args := []string{"upgrade", tool + "@latest", "--bump"}
	if ignoreAge {
		args = append(args, "--minimum-release-age", "0")
	}
	return args
}

func runMiseUpgrade(tool string, ignoreAge bool) error {
	mise, err := exec.LookPath("mise")
	if err != nil {
		return fmt.Errorf("check-usage is installed by mise, but mise is not on PATH; run `mise %s`", strings.Join(miseUpgradeArgs(tool, ignoreAge), " "))
	}
	cmd := exec.Command(mise, miseUpgradeArgs(tool, ignoreAge)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = append(os.Environ(), "MISE_YES=1")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("`mise %s` failed: %w", strings.Join(miseUpgradeArgs(tool, ignoreAge), " "), err)
	}
	return nil
}

//go:build linux

package cli

import (
	"strings"
	"testing"
)

func TestSystemdUnitGenerationOnLinux(t *testing.T) {
	unit := systemdUnitContent("/home/me/.local/bin/check-usage", "/srv/check-usage/server.json", "/home/me/.local/bin:/usr/bin")
	for _, want := range []string{
		`ExecStart="/home/me/.local/bin/check-usage" server --config "/srv/check-usage/server.json" serve`,
		`Environment="PATH=/home/me/.local/bin:/usr/bin"`,
		`Environment="CHECK_USAGE_SERVICE_BINARY=/home/me/.local/bin/check-usage"`,
		`Restart=always`,
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("unit missing %q:\n%s", want, unit)
		}
	}
}

func TestExistingUnitSettingsSurviveRewrite(t *testing.T) {
	// A unit written by v8 with a versioned mise path and an unusual config path.
	old := systemdUnitContent("/home/me/.local/share/mise/installs/github-mc-melon-tv-check-usage/8/check-usage", `/srv/odd "dir"/50%/server.json`, `/home/me/bin:/usr/bin`)
	configPath, pathEnv := existingUnitSettings(old)
	if configPath != `/srv/odd "dir"/50%/server.json` || pathEnv != "/home/me/bin:/usr/bin" {
		t.Fatalf("recovered config=%q path=%q", configPath, pathEnv)
	}
	if again := systemdUnitContent("/x/latest/check-usage", configPath, pathEnv); !strings.Contains(again, `--config "/srv/odd \"dir\"/50%%/server.json"`) {
		t.Fatalf("rewritten unit lost the config path:\n%s", again)
	}
	if configPath, _ := existingUnitSettings(systemdUnitContent("/x", "", "")); configPath != "" {
		t.Fatalf("default config path should stay implicit, got %q", configPath)
	}
}

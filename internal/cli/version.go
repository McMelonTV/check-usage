package cli

import (
	"flag"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
)

// Version and Commit are optionally set at link time via -ldflags -X.
var (
	Version = "dev"
	Commit  = ""
)

// CurrentVersion returns the build version, falling back to Go module build info.
func CurrentVersion() string {
	if v := strings.TrimSpace(Version); v != "" && v != "dev" {
		return v
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := strings.TrimSpace(info.Main.Version); v != "" && v != "(devel)" {
			return v
		}
	}
	return "dev"
}

// CurrentCommit returns the short VCS commit hash if available.
func CurrentCommit() string {
	commit := strings.TrimSpace(Commit)
	dirty := false
	if commit == "" {
		if info, ok := debug.ReadBuildInfo(); ok {
			for _, setting := range info.Settings {
				switch setting.Key {
				case "vcs.revision":
					commit = strings.TrimSpace(setting.Value)
				case "vcs.modified":
					dirty = setting.Value == "true"
				}
			}
		}
	}
	if len(commit) > 7 {
		commit = commit[:7]
	}
	if commit != "" && dirty {
		return commit + "+dirty"
	}
	return commit
}

// FormatVersion returns a human-readable version string.
func FormatVersion() string {
	v := CurrentVersion()
	if c := CurrentCommit(); c != "" && !strings.Contains(v, strings.TrimSuffix(c, "+dirty")) {
		return fmt.Sprintf("check-usage %s (%s)", v, c)
	}
	return fmt.Sprintf("check-usage %s", v)
}

func sameVersion(current, target string) bool {
	a := strings.TrimPrefix(strings.TrimSpace(current), "v")
	b := strings.TrimPrefix(strings.TrimSpace(target), "v")
	if a == "" || b == "" || a == "dev" {
		return false
	}
	return strings.EqualFold(a, b)
}

func runVersionCommand(args []string) int {
	fs := flag.NewFlagSet("check-usage version", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	setDoubleDashFlagUsage(fs)
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return 2
	}
	fmt.Println(FormatVersion())
	return 0
}

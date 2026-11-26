package cli

import (
	"context"
	"os"
	"time"
)

// serviceBinaryEnv names the binary a service unit starts. The server watches
// it and exits cleanly when it changes, so systemd restarts it on the upgrade.
const serviceBinaryEnv = "CHECK_USAGE_SERVICE_BINARY"

const serviceBinaryPollInterval = 30 * time.Second

// watchServiceBinary calls onChange once path points at a different file than
// it did when watching started: a new mise version behind the `latest`
// symlink, or a binary replaced in place by `check-usage update`. A missing or
// half-written path is ignored until a regular file appears there.
func watchServiceBinary(ctx context.Context, path string, interval time.Duration, onChange func()) {
	initial, err := os.Stat(path)
	if err != nil {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		info, err := os.Stat(path)
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 && !os.SameFile(initial, info) {
			onChange()
			return
		}
	}
}

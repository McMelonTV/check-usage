//go:build !windows

package nativecli

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// fetchAgyPlan runs interactive agy in a pseudo-terminal only for as long as
// it takes to read GetUserStatus, then kills its process group.
func fetchAgyPlan(ctx context.Context, executable, dir string, env []string) (string, error) {
	token, err := newCSRFToken()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, agyPlanTimeout)
	defer cancel()
	cmd := exec.Command(executable)
	cmd.Dir, cmd.Env = dir, withEnv(env, agyCSRFEnv, token)
	terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 120})
	if err != nil {
		return "", errors.New("cannot start agy for its plan")
	}
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		cancel() // stop polling as soon as agy exits on its own
		close(exited)
	}()
	defer func() {
		// pty.Start makes agy a session leader, so its pid is the group id.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		terminal.Close()
		<-exited
	}()
	go io.Copy(io.Discard, terminal)
	pid := cmd.Process.Pid
	return pollAgyPlan(ctx, func() []int { return listeningPorts(ctx, pid) }, token)
}

// listeningPorts lists loopback TCP ports owned by pid: /proc on Linux, lsof elsewhere.
func listeningPorts(ctx context.Context, pid int) []int {
	if ports, ok := procListeningPorts(pid); ok {
		return ports
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "lsof", "-nP", "-a", "-iTCP", "-sTCP:LISTEN", "-p", strconv.Itoa(pid), "-Fn").Output()
	if err != nil {
		return nil
	}
	var ports []int
	for line := range strings.Lines(string(output)) {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "n") {
			continue
		}
		if port, err := strconv.Atoi(line[strings.LastIndex(line, ":")+1:]); err == nil {
			ports = append(ports, port)
		}
	}
	return ports
}

func procListeningPorts(pid int) ([]int, bool) {
	entries, err := os.ReadDir(filepath.Join("/proc", strconv.Itoa(pid), "fd"))
	if err != nil {
		return nil, false
	}
	inodes := map[string]bool{}
	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "fd", entry.Name()))
		if err == nil && strings.HasPrefix(target, "socket:[") {
			inodes[strings.TrimSuffix(strings.TrimPrefix(target, "socket:["), "]")] = true
		}
	}
	var ports []int
	for _, table := range []string{"tcp", "tcp6"} {
		file, err := os.Open(filepath.Join("/proc", strconv.Itoa(pid), "net", table))
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(file)
		scanner.Scan() // header
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			// fields: sl local_address rem_address st ... inode at index 9; 0A is LISTEN.
			if len(fields) < 10 || fields[3] != "0A" || !inodes[fields[9]] {
				continue
			}
			if port, err := strconv.ParseInt(fields[1][strings.LastIndex(fields[1], ":")+1:], 16, 32); err == nil {
				ports = append(ports, int(port))
			}
		}
		file.Close()
	}
	return ports, true
}

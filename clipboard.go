package main

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/aymanbagabas/go-osc52/v2"
	"github.com/charmbracelet/x/term"
)

// copyToClipboard copies text with a native clipboard tool when one exists and
// also emits OSC 52 so terminals over SSH or without such a tool can copy it.
// native reports whether a clipboard tool accepted the text.
func copyToClipboard(text string) (native bool, err error) {
	native = copyWithClipboardTool(text)
	if term.IsTerminal(os.Stderr.Fd()) {
		sequence := osc52.New(text)
		switch {
		case os.Getenv("TMUX") != "":
			sequence = sequence.Tmux()
		case strings.HasPrefix(os.Getenv("TERM"), "screen"):
			sequence = sequence.Screen()
		}
		if _, writeErr := sequence.WriteTo(os.Stderr); writeErr == nil {
			return native, nil
		}
	}
	if native {
		return true, nil
	}
	return false, errors.New("no clipboard tool found and the terminal is not reachable")
}

func copyWithClipboardTool(text string) bool {
	for _, command := range clipboardCommands() {
		if _, err := exec.LookPath(command[0]); err != nil {
			continue
		}
		cmd := exec.Command(command[0], command[1:]...)
		cmd.Stdin = strings.NewReader(text)
		if cmd.Run() == nil {
			return true
		}
	}
	return false
}

func clipboardCommands() [][]string {
	switch runtime.GOOS {
	case "darwin":
		return [][]string{{"pbcopy"}}
	case "windows":
		return [][]string{{"clip.exe"}}
	}
	var commands [][]string
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		commands = append(commands, []string{"wl-copy"})
	}
	if os.Getenv("DISPLAY") != "" {
		commands = append(commands, []string{"xclip", "-selection", "clipboard"}, []string{"xsel", "--clipboard", "--input"})
	}
	return commands
}

// terminalHyperlink wraps text in an OSC 8 hyperlink to target.
func terminalHyperlink(target, text string) string {
	return "\x1b]8;;" + target + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

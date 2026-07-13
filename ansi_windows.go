//go:build windows

package main

import (
	"os"
	"syscall"
	"unsafe"
)

const enableVirtualTerminalProcessing = 0x0004

var (
	kernel32       = syscall.NewLazyDLL("kernel32.dll")
	getConsoleMode = kernel32.NewProc("GetConsoleMode")
	setConsoleMode = kernel32.NewProc("SetConsoleMode")
)

// configureANSIOutput enables ANSI escape-sequence handling in the classic
// Windows console. Windows Terminal already enables this mode, and redirected
// streams are left untouched because GetConsoleMode fails for non-console
// handles.
func configureANSIOutput() {
	enableANSIForFile(os.Stdout)
	enableANSIForFile(os.Stderr)
}

func enableANSIForFile(file *os.File) {
	handle := syscall.Handle(file.Fd())
	var mode uint32
	ok, _, _ := getConsoleMode.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&mode)),
	)
	if ok == 0 || mode&enableVirtualTerminalProcessing != 0 {
		return
	}

	setConsoleMode.Call(
		uintptr(handle),
		uintptr(mode|enableVirtualTerminalProcessing),
	)
}

//go:build windows

package main

import "golang.org/x/sys/windows"

// The unified executable is built with console support so CLI output behaves
// normally. A no-argument desktop launch detaches that console immediately.
func detachConsole() {
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	freeConsole := kernel32.NewProc("FreeConsole")
	_, _, _ = freeConsole.Call()
}

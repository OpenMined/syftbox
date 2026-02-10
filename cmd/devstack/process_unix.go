//go:build !windows

package main

import (
	"os"
	"syscall"
)

// processExists checks if a process is running (non-Windows).
func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

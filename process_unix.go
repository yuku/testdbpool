//go:build !windows

package testdbpool

import (
	"syscall"
)

// isProcessAlive checks if a process with given PID exists
func isProcessAlive(pid int) bool {
	// On Unix systems, sending signal 0 checks if process exists
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	if err == syscall.EPERM {
		// Process exists but we don't have permission
		return true
	}
	return false
}
//go:build !windows

package main

import (
	"os"
	"syscall"
)

// processAlive reports whether a process with the given pid is running.
// Signal 0 performs error checking without delivering a signal.
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

//go:build windows

package main

import "os"

// processAlive reports whether a process with the given pid is running.
// On Windows os.FindProcess opens the process and fails once it has exited.
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = proc.Release()
	return true
}

//go:build windows

package docker

import "os/exec"

func configureCommand(_ *exec.Cmd) {}

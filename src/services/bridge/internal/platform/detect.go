package platform

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"time"
)

type PlatformInfo struct {
	OS              string `json:"os"`
	DockerAvailable bool   `json:"docker_available"`
	KVMAvailable    bool   `json:"kvm_available"`
}

func Detect() PlatformInfo {
	return PlatformInfo{
		OS:              runtime.GOOS,
		DockerAvailable: detectDocker(),
		KVMAvailable:    detectKVM(),
	}
}

func detectDocker() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Try docker from PATH first, then common locations.
	for _, bin := range []string{"docker", "/usr/local/bin/docker", "/usr/bin/docker"} {
		cmd := exec.CommandContext(ctx, bin, "info")
		cmd.Stdout = nil
		cmd.Stderr = nil
		if cmd.Run() == nil {
			return true
		}
	}
	return false
}

func detectKVM() bool {
	if runtime.GOOS != "linux" {
		return false
	}

	info, err := os.Stat("/dev/kvm")
	if err != nil {
		return false
	}

	// Verify it's accessible (not just that it exists)
	f, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err != nil {
		// Fall back: file exists but may not be writable, check read access
		f, err = os.Open("/dev/kvm")
		if err != nil {
			return false
		}
		f.Close()
		return true
	}
	f.Close()
	_ = info
	return true
}

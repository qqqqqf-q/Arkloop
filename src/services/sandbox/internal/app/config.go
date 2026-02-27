package app

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

const (
	sandboxAddrEnv            = "ARKLOOP_SANDBOX_ADDR"
	firecrackerBinEnv         = "ARKLOOP_FIRECRACKER_BIN"
	kernelImagePathEnv        = "ARKLOOP_SANDBOX_KERNEL_IMAGE"
	rootfsPathEnv             = "ARKLOOP_SANDBOX_ROOTFS"
	socketBaseDirEnv          = "ARKLOOP_SANDBOX_SOCKET_DIR"
	bootTimeoutSecondsEnv     = "ARKLOOP_SANDBOX_BOOT_TIMEOUT_SECONDS"
	guestAgentPortEnv         = "ARKLOOP_SANDBOX_AGENT_PORT"
	maxSessionsEnv            = "ARKLOOP_SANDBOX_MAX_SESSIONS"
	s3EndpointEnv             = "ARKLOOP_S3_ENDPOINT"
	s3AccessKeyEnv            = "ARKLOOP_S3_ACCESS_KEY"
	s3SecretKeyEnv            = "ARKLOOP_S3_SECRET_KEY"
	templatesPathEnv          = "ARKLOOP_SANDBOX_TEMPLATES_PATH"
)

type Config struct {
	Addr               string
	FirecrackerBin     string
	KernelImagePath    string
	RootfsPath         string
	SocketBaseDir      string
	BootTimeoutSeconds int
	GuestAgentPort     uint32
	MaxSessions        int
	S3Endpoint         string
	S3AccessKey        string
	S3SecretKey        string
	TemplatesPath      string
}

func DefaultConfig() Config {
	return Config{
		Addr:               "0.0.0.0:8002",
		FirecrackerBin:     "/usr/bin/firecracker",
		KernelImagePath:    "/opt/sandbox/vmlinux",
		RootfsPath:         "/opt/sandbox/rootfs.ext4",
		SocketBaseDir:      "/run/sandbox",
		BootTimeoutSeconds: 30,
		GuestAgentPort:     8080,
		MaxSessions:        50,
		TemplatesPath:      "/opt/sandbox/templates.json",
	}
}

func LoadConfigFromEnv() (Config, error) {
	cfg := DefaultConfig()

	if raw := strings.TrimSpace(os.Getenv(sandboxAddrEnv)); raw != "" {
		cfg.Addr = raw
	}
	if raw := strings.TrimSpace(os.Getenv(firecrackerBinEnv)); raw != "" {
		cfg.FirecrackerBin = raw
	}
	if raw := strings.TrimSpace(os.Getenv(kernelImagePathEnv)); raw != "" {
		cfg.KernelImagePath = raw
	}
	if raw := strings.TrimSpace(os.Getenv(rootfsPathEnv)); raw != "" {
		cfg.RootfsPath = raw
	}
	if raw := strings.TrimSpace(os.Getenv(socketBaseDirEnv)); raw != "" {
		cfg.SocketBaseDir = raw
	}
	if raw := strings.TrimSpace(os.Getenv(bootTimeoutSecondsEnv)); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v <= 0 {
			return Config{}, fmt.Errorf("%s: must be a positive integer", bootTimeoutSecondsEnv)
		}
		cfg.BootTimeoutSeconds = v
	}
	if raw := strings.TrimSpace(os.Getenv(guestAgentPortEnv)); raw != "" {
		v, err := strconv.ParseUint(raw, 10, 32)
		if err != nil {
			return Config{}, fmt.Errorf("%s: must be a valid port number", guestAgentPortEnv)
		}
		cfg.GuestAgentPort = uint32(v)
	}
	if raw := strings.TrimSpace(os.Getenv(maxSessionsEnv)); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v <= 0 {
			return Config{}, fmt.Errorf("%s: must be a positive integer", maxSessionsEnv)
		}
		cfg.MaxSessions = v
	}
	if raw := strings.TrimSpace(os.Getenv(s3EndpointEnv)); raw != "" {
		cfg.S3Endpoint = raw
	}
	if raw := strings.TrimSpace(os.Getenv(s3AccessKeyEnv)); raw != "" {
		cfg.S3AccessKey = raw
	}
	if raw := strings.TrimSpace(os.Getenv(s3SecretKeyEnv)); raw != "" {
		cfg.S3SecretKey = raw
	}
	if raw := strings.TrimSpace(os.Getenv(templatesPathEnv)); raw != "" {
		cfg.TemplatesPath = raw
	}

	return cfg, cfg.Validate()
}

func (c Config) Validate() error {
	if _, err := net.ResolveTCPAddr("tcp", c.Addr); err != nil {
		return fmt.Errorf("addr invalid: %w", err)
	}
	if c.BootTimeoutSeconds <= 0 {
		return fmt.Errorf("boot_timeout_seconds must be positive")
	}
	if c.MaxSessions <= 0 {
		return fmt.Errorf("max_sessions must be positive")
	}
	return nil
}

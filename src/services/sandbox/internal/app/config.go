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

	warmLiteEnv               = "ARKLOOP_SANDBOX_WARM_LITE"
	warmProEnv                = "ARKLOOP_SANDBOX_WARM_PRO"
	warmUltraEnv              = "ARKLOOP_SANDBOX_WARM_ULTRA"
	refillIntervalEnv         = "ARKLOOP_SANDBOX_REFILL_INTERVAL"
	refillConcurrencyEnv      = "ARKLOOP_SANDBOX_REFILL_CONCURRENCY"
	idleTimeoutLiteEnv        = "ARKLOOP_SANDBOX_IDLE_TIMEOUT_LITE"
	idleTimeoutProEnv         = "ARKLOOP_SANDBOX_IDLE_TIMEOUT_PRO"
	idleTimeoutUltraEnv       = "ARKLOOP_SANDBOX_IDLE_TIMEOUT_ULTRA"
	maxLifetimeEnv            = "ARKLOOP_SANDBOX_MAX_LIFETIME"
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

	// Warm pool: 各 tier 的预热 VM 数量
	WarmLite  int
	WarmPro   int
	WarmUltra int

	// Warm pool: 补充策略
	RefillIntervalSeconds int
	RefillConcurrency     int

	// Session 超时: 各 tier 空闲超时（秒）
	IdleTimeoutLite  int
	IdleTimeoutPro   int
	IdleTimeoutUltra int

	// Session 超时: 最大存活时间（秒），所有 tier 统一
	MaxLifetimeSeconds int
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

		WarmLite:              3,
		WarmPro:               2,
		WarmUltra:             1,
		RefillIntervalSeconds: 5,
		RefillConcurrency:     2,
		IdleTimeoutLite:       180,
		IdleTimeoutPro:        300,
		IdleTimeoutUltra:      600,
		MaxLifetimeSeconds:    1800,
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

	// warm pool
	if v, err := envPositiveInt(warmLiteEnv); err != nil {
		return Config{}, err
	} else if v > 0 {
		cfg.WarmLite = v
	}
	if v, err := envPositiveInt(warmProEnv); err != nil {
		return Config{}, err
	} else if v > 0 {
		cfg.WarmPro = v
	}
	if v, err := envPositiveInt(warmUltraEnv); err != nil {
		return Config{}, err
	} else if v > 0 {
		cfg.WarmUltra = v
	}
	if v, err := envPositiveInt(refillIntervalEnv); err != nil {
		return Config{}, err
	} else if v > 0 {
		cfg.RefillIntervalSeconds = v
	}
	if v, err := envPositiveInt(refillConcurrencyEnv); err != nil {
		return Config{}, err
	} else if v > 0 {
		cfg.RefillConcurrency = v
	}

	// session timeout
	if v, err := envPositiveInt(idleTimeoutLiteEnv); err != nil {
		return Config{}, err
	} else if v > 0 {
		cfg.IdleTimeoutLite = v
	}
	if v, err := envPositiveInt(idleTimeoutProEnv); err != nil {
		return Config{}, err
	} else if v > 0 {
		cfg.IdleTimeoutPro = v
	}
	if v, err := envPositiveInt(idleTimeoutUltraEnv); err != nil {
		return Config{}, err
	} else if v > 0 {
		cfg.IdleTimeoutUltra = v
	}
	if v, err := envPositiveInt(maxLifetimeEnv); err != nil {
		return Config{}, err
	} else if v > 0 {
		cfg.MaxLifetimeSeconds = v
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
	if c.RefillIntervalSeconds <= 0 {
		return fmt.Errorf("refill_interval must be positive")
	}
	if c.RefillConcurrency <= 0 {
		return fmt.Errorf("refill_concurrency must be positive")
	}
	if c.MaxLifetimeSeconds <= 0 {
		return fmt.Errorf("max_lifetime must be positive")
	}
	return nil
}

// WarmSizes 返回各 tier 预热数量的 map。
func (c Config) WarmSizes() map[string]int {
	return map[string]int{
		"lite":  c.WarmLite,
		"pro":   c.WarmPro,
		"ultra": c.WarmUltra,
	}
}

// IdleTimeoutSeconds 返回指定 tier 的空闲超时（秒）。
func (c Config) IdleTimeoutSeconds(tier string) int {
	switch tier {
	case "pro":
		return c.IdleTimeoutPro
	case "ultra":
		return c.IdleTimeoutUltra
	default:
		return c.IdleTimeoutLite
	}
}

// envPositiveInt 从 ENV 读取正整数。未设置返回 0, nil；格式错误返回错误。
func envPositiveInt(key string) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("%s: must be a positive integer", key)
	}
	return v, nil
}

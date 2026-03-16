//go:build !desktop

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	sharedconfig "arkloop/services/shared/config"
	"arkloop/services/shared/objectstore"

	"github.com/jackc/pgx/v5/pgxpool"
)

// 部署级别的 ENV（文件路径、地址、凭证 -- 不进 registry，不能从 console 改）
const (
	sandboxAddrEnv           = "ARKLOOP_SANDBOX_ADDR"
	sandboxAuthTokenEnv      = "ARKLOOP_SANDBOX_AUTH_TOKEN"
	restoreTTLEnv            = "ARKLOOP_SANDBOX_RESTORE_TTL_DAYS"
	legacySessionStateTTLEnv = "ARKLOOP_SANDBOX_SESSION_STATE_TTL_DAYS"
	flushDebounceMSEnv       = "ARKLOOP_SANDBOX_FLUSH_DEBOUNCE_MS"
	flushMaxDirtyAgeMSEnv    = "ARKLOOP_SANDBOX_FLUSH_MAX_DIRTY_AGE_MS"
	flushForceBytesEnv       = "ARKLOOP_SANDBOX_FLUSH_FORCE_BYTES_THRESHOLD"
	flushForceCountEnv       = "ARKLOOP_SANDBOX_FLUSH_FORCE_COUNT_THRESHOLD"
	allowEgressEnv           = "ARKLOOP_SANDBOX_ALLOW_EGRESS"
	dockerNetworkEnv         = "ARKLOOP_SANDBOX_DOCKER_NETWORK"
	firecrackerBinEnv        = "ARKLOOP_FIRECRACKER_BIN"
	kernelImagePathEnv       = "ARKLOOP_SANDBOX_KERNEL_IMAGE"
	initrdPathEnv            = "ARKLOOP_SANDBOX_INITRD"
	rootfsPathEnv            = "ARKLOOP_SANDBOX_ROOTFS"
	socketBaseDirEnv         = "ARKLOOP_SANDBOX_SOCKET_DIR"
	templatesPathEnv         = "ARKLOOP_SANDBOX_TEMPLATES_PATH"
	firecrackerIfaceEnv      = "ARKLOOP_SANDBOX_EGRESS_INTERFACE"
	firecrackerTapEnv        = "ARKLOOP_SANDBOX_FIRECRACKER_TAP_PREFIX"
	firecrackerCIDREnv       = "ARKLOOP_SANDBOX_FIRECRACKER_TAP_CIDR"
	firecrackerDNSEnv        = "ARKLOOP_SANDBOX_FIRECRACKER_DNS"
	s3EndpointEnv            = "ARKLOOP_S3_ENDPOINT"
	s3AccessKeyEnv           = "ARKLOOP_S3_ACCESS_KEY"
	s3SecretKeyEnv           = "ARKLOOP_S3_SECRET_KEY"
)

// Provider 标识 sandbox 后端类型。
const (
	ProviderFirecracker = "firecracker"
	ProviderDocker      = "docker"
	ProviderVz          = "vz"
	ProviderLocal       = "local"
)

type Config struct {
	Addr                       string
	AuthToken                  string // 服务间认证 Bearer token，空则跳过校验（仅限开发环境）
	Provider                   string // "firecracker" | "docker"
	FirecrackerBin             string
	KernelImagePath            string
	InitrdPath                 string // optional initramfs for Vz provider
	RootfsPath                 string
	SocketBaseDir              string
	BootTimeoutSeconds         int
	GuestAgentPort             uint32
	MaxSessions                int
	S3Endpoint                 string
	S3AccessKey                string
	S3SecretKey                string
	StorageBackend             string
	StorageRoot                string
	RestoreTTLDays             int
	FlushDebounceMS            int
	FlushMaxDirtyAgeMS         int
	FlushForceBytesThreshold   int
	FlushForceCountThreshold   int
	TemplatesPath              string
	DockerImage                string // Docker 后端 lite/pro 使用的 sandbox-agent 镜像
	BrowserDockerImage         string // Docker 后端 browser 使用的 sandbox-agent 镜像
	AllowEgress                bool
	DockerNetwork              string // agent 容器加入的 Docker 网络（compose 桥接网络）
	FirecrackerEgressInterface string
	FirecrackerTapPrefix       string
	FirecrackerTapCIDR         string
	FirecrackerDNS             []string

	// Warm pool: 各 tier 的预热 VM 数量
	WarmLite    int
	WarmPro     int
	WarmBrowser int

	// Warm pool: 补充策略
	RefillIntervalSeconds int
	RefillConcurrency     int

	// Session 超时: 各 tier 空闲超时（秒）
	IdleTimeoutLite    int
	IdleTimeoutPro     int
	IdleTimeoutBrowser int

	// Session 超时: 最大存活时间（秒）
	MaxLifetimeSeconds        int
	MaxLifetimeBrowserSeconds int
}

func DefaultConfig() Config {
	return Config{
		Addr:                       "0.0.0.0:19002",
		Provider:                   ProviderFirecracker,
		FirecrackerBin:             "/usr/bin/firecracker",
		KernelImagePath:            "/opt/sandbox/vmlinux",
		RootfsPath:                 "/opt/sandbox/rootfs.ext4",
		SocketBaseDir:              "/run/sandbox",
		BootTimeoutSeconds:         30,
		GuestAgentPort:             8080,
		MaxSessions:                50,
		RestoreTTLDays:             7,
		FlushDebounceMS:            2000,
		FlushMaxDirtyAgeMS:         15000,
		FlushForceBytesThreshold:   16 << 20,
		FlushForceCountThreshold:   512,
		TemplatesPath:              "/opt/sandbox/templates.json",
		DockerImage:                "arkloop/sandbox-agent:latest",
		BrowserDockerImage:         "arkloop/sandbox-browser:dev",
		AllowEgress:                true,
		DockerNetwork:              "arkloop_sandbox_agent_egress",
		FirecrackerEgressInterface: "eth0",
		FirecrackerTapPrefix:       "arktap",
		FirecrackerTapCIDR:         "172.29.0.0/16",
		FirecrackerDNS:             []string{"1.1.1.1", "8.8.8.8"},

		WarmLite:                  3,
		WarmPro:                   2,
		WarmBrowser:               1,
		RefillIntervalSeconds:     5,
		RefillConcurrency:         2,
		IdleTimeoutLite:           180,
		IdleTimeoutPro:            300,
		IdleTimeoutBrowser:        120,
		MaxLifetimeSeconds:        1800,
		MaxLifetimeBrowserSeconds: 600,
	}
}

func LoadConfigFromEnv() (Config, error) {
	cfg := DefaultConfig()

	// --- 部署级 ENV（不进 registry）---
	if raw := strings.TrimSpace(os.Getenv(sandboxAddrEnv)); raw != "" {
		cfg.Addr = raw
	}
	cfg.AuthToken = strings.TrimSpace(os.Getenv(sandboxAuthTokenEnv))
	if raw, ok := lookupEnvFirst(restoreTTLEnv, legacySessionStateTTLEnv); ok {
		value, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || value < 0 {
			return cfg, fmt.Errorf("restore_ttl_days must be zero or positive")
		}
		cfg.RestoreTTLDays = value
	}
	if raw, ok := os.LookupEnv(flushDebounceMSEnv); ok {
		value, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || value <= 0 {
			return cfg, fmt.Errorf("flush_debounce_ms must be positive")
		}
		cfg.FlushDebounceMS = value
	}
	if raw, ok := os.LookupEnv(flushMaxDirtyAgeMSEnv); ok {
		value, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || value <= 0 {
			return cfg, fmt.Errorf("flush_max_dirty_age_ms must be positive")
		}
		cfg.FlushMaxDirtyAgeMS = value
	}
	if raw, ok := os.LookupEnv(flushForceBytesEnv); ok {
		value, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || value <= 0 {
			return cfg, fmt.Errorf("flush_force_bytes_threshold must be positive")
		}
		cfg.FlushForceBytesThreshold = value
	}
	if raw, ok := os.LookupEnv(flushForceCountEnv); ok {
		value, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || value <= 0 {
			return cfg, fmt.Errorf("flush_force_count_threshold must be positive")
		}
		cfg.FlushForceCountThreshold = value
	}
	if raw, ok := os.LookupEnv(allowEgressEnv); ok {
		value, err := strconv.ParseBool(strings.TrimSpace(raw))
		if err != nil {
			return cfg, fmt.Errorf("allow_egress must be true/false")
		}
		cfg.AllowEgress = value
	}
	if raw := strings.TrimSpace(os.Getenv(dockerNetworkEnv)); raw != "" {
		cfg.DockerNetwork = raw
	}
	if raw := strings.TrimSpace(os.Getenv(firecrackerIfaceEnv)); raw != "" {
		cfg.FirecrackerEgressInterface = raw
	}
	if raw := strings.TrimSpace(os.Getenv(firecrackerTapEnv)); raw != "" {
		cfg.FirecrackerTapPrefix = raw
	}
	if raw := strings.TrimSpace(os.Getenv(firecrackerCIDREnv)); raw != "" {
		cfg.FirecrackerTapCIDR = raw
	}
	if raw := strings.TrimSpace(os.Getenv(firecrackerDNSEnv)); raw != "" {
		cfg.FirecrackerDNS = splitCSV(raw)
	}
	if raw := strings.TrimSpace(os.Getenv(firecrackerBinEnv)); raw != "" {
		cfg.FirecrackerBin = raw
	}
	if raw := strings.TrimSpace(os.Getenv(kernelImagePathEnv)); raw != "" {
		cfg.KernelImagePath = raw
	}
	if raw := strings.TrimSpace(os.Getenv(initrdPathEnv)); raw != "" {
		cfg.InitrdPath = raw
	}
	if raw := strings.TrimSpace(os.Getenv(rootfsPathEnv)); raw != "" {
		cfg.RootfsPath = raw
	}
	if raw := strings.TrimSpace(os.Getenv(socketBaseDirEnv)); raw != "" {
		cfg.SocketBaseDir = raw
	}
	if raw := strings.TrimSpace(os.Getenv(templatesPathEnv)); raw != "" {
		cfg.TemplatesPath = raw
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
	if raw := strings.TrimSpace(os.Getenv(objectstore.StorageBackendEnv)); raw != "" {
		cfg.StorageBackend = raw
	}
	if raw := strings.TrimSpace(os.Getenv(objectstore.StorageRootEnv)); raw != "" {
		cfg.StorageRoot = raw
	}

	// --- Registry 级配置（优先级：ENV > DB > Default）---
	// 尝试连接数据库以构建 Resolver；连不上则退化为纯 ENV + Default
	dbURL := strings.TrimSpace(os.Getenv("ARKLOOP_DATABASE_URL"))
	registry := sharedconfig.DefaultRegistry()

	var resolver *sharedconfig.ResolverImpl
	var dbPool *pgxpool.Pool

	if dbURL != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		poolCfg, err := pgxpool.ParseConfig(dbURL)
		if err != nil {
			writeConfigWarn("platform_settings_parse_failed", err)
		} else {
			poolCfg.MaxConns = 2
			dbPool, err = pgxpool.NewWithConfig(ctx, poolCfg)
			if err != nil {
				writeConfigWarn("platform_settings_connect_failed", err)
			}
		}
	}

	if dbPool != nil {
		resolver, _ = sharedconfig.NewResolver(registry, sharedconfig.NewPGXStore(dbPool), nil, 0)
		defer dbPool.Close()
	} else {
		resolver, _ = sharedconfig.NewResolver(registry, nil, nil, 0)
	}

	ctx := context.Background()
	scope := sharedconfig.Scope{}

	resolveStr := func(key string) string {
		raw, err := resolver.Resolve(ctx, key, scope)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(raw)
	}
	resolveInt := func(key string) int {
		raw := resolveStr(key)
		if raw == "" {
			return 0
		}
		v, err := strconv.Atoi(raw)
		if err != nil || v <= 0 {
			return 0
		}
		return v
	}
	resolveBool := func(key string) *bool {
		raw := resolveStr(key)
		if raw == "" {
			return nil
		}
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return nil
		}
		return &v
	}
	resolveNonNegativeInt := func(key string) (int, bool) {
		raw := resolveStr(key)
		if raw == "" {
			return 0, false
		}
		v, err := strconv.Atoi(raw)
		if err != nil || v < 0 {
			return 0, false
		}
		return v, true
	}

	if v := resolveStr("sandbox.provider"); v != "" {
		cfg.Provider = v
	}
	if v := resolveStr("sandbox.docker_image"); v != "" {
		cfg.DockerImage = v
	}
	if v := resolveStr("sandbox.browser_docker_image"); v != "" {
		cfg.BrowserDockerImage = v
	}
	if v := resolveBool("sandbox.allow_egress"); v != nil {
		cfg.AllowEgress = *v
	}
	if v := resolveInt("sandbox.max_sessions"); v > 0 {
		cfg.MaxSessions = v
	}
	if v := resolveInt("sandbox.agent_port"); v > 0 {
		cfg.GuestAgentPort = uint32(v)
	}
	if v := resolveInt("sandbox.boot_timeout_s"); v > 0 {
		cfg.BootTimeoutSeconds = v
	}
	if v, ok := resolveNonNegativeInt("sandbox.warm_lite"); ok {
		cfg.WarmLite = v
	}
	if v, ok := resolveNonNegativeInt("sandbox.warm_pro"); ok {
		cfg.WarmPro = v
	}
	if v, ok := resolveNonNegativeInt("sandbox.warm_browser"); ok {
		cfg.WarmBrowser = v
	}
	if v := resolveInt("sandbox.refill_interval_s"); v > 0 {
		cfg.RefillIntervalSeconds = v
	}
	if v := resolveInt("sandbox.refill_concurrency"); v > 0 {
		cfg.RefillConcurrency = v
	}
	if v := resolveInt("sandbox.idle_timeout_lite_s"); v > 0 {
		cfg.IdleTimeoutLite = v
	}
	if v := resolveInt("sandbox.idle_timeout_pro_s"); v > 0 {
		cfg.IdleTimeoutPro = v
	}
	if v := resolveInt("sandbox.idle_timeout_browser_s"); v > 0 {
		cfg.IdleTimeoutBrowser = v
	}
	if v := resolveInt("sandbox.max_lifetime_s"); v > 0 {
		cfg.MaxLifetimeSeconds = v
	}
	if v := resolveInt("sandbox.max_lifetime_browser_s"); v > 0 {
		cfg.MaxLifetimeBrowserSeconds = v
	}
	if v, ok := resolveNonNegativeInt("sandbox.restore_ttl_days"); ok {
		cfg.RestoreTTLDays = v
	}
	if v := resolveInt("sandbox.flush_debounce_ms"); v > 0 {
		cfg.FlushDebounceMS = v
	}
	if v := resolveInt("sandbox.flush_max_dirty_age_ms"); v > 0 {
		cfg.FlushMaxDirtyAgeMS = v
	}
	if v := resolveInt("sandbox.flush_force_bytes_threshold"); v > 0 {
		cfg.FlushForceBytesThreshold = v
	}
	if v := resolveInt("sandbox.flush_force_count_threshold"); v > 0 {
		cfg.FlushForceCountThreshold = v
	}

	return cfg, cfg.Validate()
}

func (c Config) Validate() error {
	if _, err := net.ResolveTCPAddr("tcp", c.Addr); err != nil {
		return fmt.Errorf("addr invalid: %w", err)
	}
	switch c.Provider {
	case ProviderFirecracker, ProviderDocker, ProviderVz, ProviderLocal:
	default:
		return fmt.Errorf("provider must be %q, %q, %q, or %q", ProviderFirecracker, ProviderDocker, ProviderVz, ProviderLocal)
	}
	if c.Provider == ProviderLocal {
		return nil
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
	if c.MaxLifetimeBrowserSeconds <= 0 {
		return fmt.Errorf("max_lifetime_browser must be positive")
	}
	if c.RestoreTTLDays < 0 {
		return fmt.Errorf("restore_ttl_days must be zero or positive")
	}
	if c.FlushDebounceMS <= 0 {
		return fmt.Errorf("flush_debounce_ms must be positive")
	}
	if c.FlushMaxDirtyAgeMS <= 0 {
		return fmt.Errorf("flush_max_dirty_age_ms must be positive")
	}
	if c.FlushForceBytesThreshold <= 0 {
		return fmt.Errorf("flush_force_bytes_threshold must be positive")
	}
	if c.FlushForceCountThreshold <= 0 {
		return fmt.Errorf("flush_force_count_threshold must be positive")
	}
	if c.Provider == ProviderDocker {
		if strings.TrimSpace(c.DockerNetwork) == "" {
			return fmt.Errorf("docker_network must not be empty")
		}
		if strings.TrimSpace(c.DockerImage) == "" {
			return fmt.Errorf("docker_image must not be empty")
		}
		if strings.TrimSpace(c.BrowserDockerImage) == "" {
			return fmt.Errorf("browser_docker_image must not be empty")
		}
		if c.WarmBrowser > 0 && !c.AllowEgress {
			return fmt.Errorf("browser warm pool requires allow_egress=true")
		}
	}
	if c.Provider == ProviderFirecracker {
		if strings.TrimSpace(c.FirecrackerEgressInterface) == "" {
			return fmt.Errorf("firecracker_egress_interface must not be empty")
		}
		if strings.TrimSpace(c.FirecrackerTapPrefix) == "" {
			return fmt.Errorf("firecracker_tap_prefix must not be empty")
		}
		if len(c.FirecrackerTapPrefix) > 10 {
			return fmt.Errorf("firecracker_tap_prefix must be 10 chars or shorter")
		}
		if _, _, err := net.ParseCIDR(c.FirecrackerTapCIDR); err != nil {
			return fmt.Errorf("firecracker_tap_cidr invalid: %w", err)
		}
		for _, ns := range c.FirecrackerDNS {
			if net.ParseIP(strings.TrimSpace(ns)) == nil {
				return fmt.Errorf("firecracker_dns contains invalid ip %q", ns)
			}
		}
	}
	if c.Provider == ProviderVz {
		if strings.TrimSpace(c.KernelImagePath) == "" {
			return fmt.Errorf("kernel_image_path must not be empty for vz provider")
		}
		if strings.TrimSpace(c.RootfsPath) == "" {
			return fmt.Errorf("rootfs_path must not be empty for vz provider")
		}
	}
	return nil
}

// WarmSizes 返回各 tier 预热数量的 map。
func (c Config) WarmSizes() map[string]int {
	return map[string]int{
		"lite":    c.WarmLite,
		"pro":     c.WarmPro,
		"browser": c.WarmBrowser,
	}
}

// IdleTimeoutSeconds 返回指定 tier 的空闲超时（秒）。
func (c Config) IdleTimeoutSeconds(tier string) int {
	switch tier {
	case "pro":
		return c.IdleTimeoutPro
	case "browser":
		return c.IdleTimeoutBrowser
	default:
		return c.IdleTimeoutLite
	}
}

// MaxLifetimeSecondsFor 返回指定 tier 的最大存活时间（秒）。
func (c Config) MaxLifetimeSecondsFor(tier string) int {
	switch tier {
	case "browser":
		return c.MaxLifetimeBrowserSeconds
	default:
		return c.MaxLifetimeSeconds
	}
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		result = append(result, part)
	}
	return result
}

func writeConfigWarn(event string, err error) {
	if err == nil {
		return
	}
	raw, _ := json.Marshal(map[string]any{
		"level": "warn",
		"event": event,
		"error": err.Error(),
	})
	_, _ = os.Stderr.Write(append(raw, '\n'))
}

func lookupEnvFirst(keys ...string) (string, bool) {
	for _, key := range keys {
		if raw, ok := os.LookupEnv(key); ok {
			return raw, true
		}
	}
	return "", false
}

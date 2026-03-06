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

	"github.com/jackc/pgx/v5/pgxpool"
)

// 部署级别的 ENV（文件路径、地址、凭证 -- 不进 registry，不能从 console 改）
const (
	sandboxAddrEnv       = "ARKLOOP_SANDBOX_ADDR"
	sandboxAuthTokenEnv  = "ARKLOOP_SANDBOX_AUTH_TOKEN"
	sessionStateTTLEnv   = "ARKLOOP_SANDBOX_SESSION_STATE_TTL_DAYS"
	dockerAllowEgressEnv = "ARKLOOP_SANDBOX_DOCKER_ALLOW_EGRESS"
	dockerNetworkEnv     = "ARKLOOP_SANDBOX_DOCKER_NETWORK"
	firecrackerBinEnv    = "ARKLOOP_FIRECRACKER_BIN"
	kernelImagePathEnv   = "ARKLOOP_SANDBOX_KERNEL_IMAGE"
	rootfsPathEnv        = "ARKLOOP_SANDBOX_ROOTFS"
	socketBaseDirEnv     = "ARKLOOP_SANDBOX_SOCKET_DIR"
	templatesPathEnv     = "ARKLOOP_SANDBOX_TEMPLATES_PATH"
	s3EndpointEnv        = "ARKLOOP_S3_ENDPOINT"
	s3AccessKeyEnv       = "ARKLOOP_S3_ACCESS_KEY"
	s3SecretKeyEnv       = "ARKLOOP_S3_SECRET_KEY"
)

// Provider 标识 sandbox 后端类型。
const (
	ProviderFirecracker = "firecracker"
	ProviderDocker      = "docker"
)

type Config struct {
	Addr                string
	AuthToken           string // 服务间认证 Bearer token，空则跳过校验（仅限开发环境）
	Provider            string // "firecracker" | "docker"
	FirecrackerBin      string
	KernelImagePath     string
	RootfsPath          string
	SocketBaseDir       string
	BootTimeoutSeconds  int
	GuestAgentPort      uint32
	MaxSessions         int
	S3Endpoint          string
	S3AccessKey         string
	S3SecretKey         string
	SessionStateTTLDays int
	TemplatesPath       string
	DockerImage         string // Docker 后端使用的 sandbox-agent 镜像
	DockerAllowEgress   bool
	DockerNetwork       string // agent 容器加入的 Docker 网络（compose 桥接网络）

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
		Addr:                "0.0.0.0:8002",
		Provider:            ProviderFirecracker,
		FirecrackerBin:      "/usr/bin/firecracker",
		KernelImagePath:     "/opt/sandbox/vmlinux",
		RootfsPath:          "/opt/sandbox/rootfs.ext4",
		SocketBaseDir:       "/run/sandbox",
		BootTimeoutSeconds:  30,
		GuestAgentPort:      8080,
		MaxSessions:         50,
		SessionStateTTLDays: 7,
		TemplatesPath:       "/opt/sandbox/templates.json",
		DockerImage:         "arkloop/sandbox-agent:latest",
		DockerAllowEgress:   false,
		DockerNetwork:       "",

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

	// --- 部署级 ENV（不进 registry）---
	if raw := strings.TrimSpace(os.Getenv(sandboxAddrEnv)); raw != "" {
		cfg.Addr = raw
	}
	cfg.AuthToken = strings.TrimSpace(os.Getenv(sandboxAuthTokenEnv))
	if raw, ok := os.LookupEnv(sessionStateTTLEnv); ok {
		value, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || value < 0 {
			return cfg, fmt.Errorf("session_state_ttl_days must be zero or positive")
		}
		cfg.SessionStateTTLDays = value
	}
	if raw, ok := os.LookupEnv(dockerAllowEgressEnv); ok {
		value, err := strconv.ParseBool(strings.TrimSpace(raw))
		if err != nil {
			return cfg, fmt.Errorf("docker_allow_egress must be true/false")
		}
		cfg.DockerAllowEgress = value
	}
	if raw := strings.TrimSpace(os.Getenv(dockerNetworkEnv)); raw != "" {
		cfg.DockerNetwork = raw
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

	if v := resolveStr("sandbox.provider"); v != "" {
		cfg.Provider = v
	}
	if v := resolveStr("sandbox.docker_image"); v != "" {
		cfg.DockerImage = v
	}
	if v := resolveBool("sandbox.docker_allow_egress"); v != nil {
		cfg.DockerAllowEgress = *v
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
	if v := resolveInt("sandbox.warm_lite"); v > 0 {
		cfg.WarmLite = v
	}
	if v := resolveInt("sandbox.warm_pro"); v > 0 {
		cfg.WarmPro = v
	}
	if v := resolveInt("sandbox.warm_ultra"); v > 0 {
		cfg.WarmUltra = v
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
	if v := resolveInt("sandbox.idle_timeout_ultra_s"); v > 0 {
		cfg.IdleTimeoutUltra = v
	}
	if v := resolveInt("sandbox.max_lifetime_s"); v > 0 {
		cfg.MaxLifetimeSeconds = v
	}

	return cfg, cfg.Validate()
}

func (c Config) Validate() error {
	if _, err := net.ResolveTCPAddr("tcp", c.Addr); err != nil {
		return fmt.Errorf("addr invalid: %w", err)
	}
	switch c.Provider {
	case ProviderFirecracker, ProviderDocker:
	default:
		return fmt.Errorf("provider must be %q or %q", ProviderFirecracker, ProviderDocker)
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
	if c.SessionStateTTLDays < 0 {
		return fmt.Errorf("session_state_ttl_days must be zero or positive")
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

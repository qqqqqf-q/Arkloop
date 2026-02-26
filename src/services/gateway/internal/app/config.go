package app

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"arkloop/services/gateway/internal/ratelimit"
)

const (
	gatewayAddrEnv     = "ARKLOOP_GATEWAY_ADDR"
	gatewayUpstreamEnv = "ARKLOOP_GATEWAY_UPSTREAM"
	redisURLEnv        = "ARKLOOP_REDIS_URL"
	jwtSecretEnv       = "ARKLOOP_AUTH_JWT_SECRET"

	// IP 透传模式：direct | cloudflare | trusted_proxy
	ipModeEnv = "ARKLOOP_GATEWAY_IP_MODE"
	// 逗号分隔的可信代理 CIDR 列表，cloudflare/trusted_proxy 模式使用
	trustedCIDRsEnv = "ARKLOOP_GATEWAY_TRUSTED_CIDRS"
	// MaxMind GeoLite2 数据库文件路径，留空则禁用 GeoIP
	geoIPDBPathEnv = "ARKLOOP_GEOIP_DB_PATH"
	// MaxMind License Key，配置后自动下载和每日更新 GeoLite2 数据库
	geoIPLicenseKeyEnv = "ARKLOOP_GEOIP_LICENSE_KEY"
	// 风险评分拒绝阈值（0-100），0 表示只记录不拒绝
	riskRejectThresholdEnv = "ARKLOOP_GATEWAY_RISK_REJECT_THRESHOLD"

	defaultAddr       = "0.0.0.0:8000"
	defaultUpstream   = "http://127.0.0.1:8001"
	defaultGeoIPDBDir = "/data/geoip"
)

// IPMode 定义 Gateway 的 IP 来源信任策略。
type IPMode string

const (
	IPModeDirect       IPMode = "direct"        // 直连：只信任 RemoteAddr
	IPModeCloudflare   IPMode = "cloudflare"     // Cloudflare 前置：读 CF-Connecting-IP
	IPModeTrustedProxy IPMode = "trusted_proxy"  // 通用可信代理：读 XFF 最左端
)

type Config struct {
	Addr      string
	Upstream  string
	RedisURL  string
	JWTSecret string
	RateLimit ratelimit.Config

	IPMode              IPMode
	TrustedCIDRs        []string // CIDR 字符串列表
	GeoIPDBPath         string
	GeoIPLicenseKey     string
	RiskRejectThreshold int // 0 = 禁用拒绝
}

func DefaultConfig() Config {
	return Config{
		Addr:      defaultAddr,
		Upstream:  defaultUpstream,
		RateLimit: ratelimit.DefaultConfig(),
	}
}

func LoadConfigFromEnv() (Config, error) {
	cfg := DefaultConfig()

	if raw := strings.TrimSpace(os.Getenv(gatewayAddrEnv)); raw != "" {
		cfg.Addr = raw
	}
	if raw := strings.TrimSpace(os.Getenv(gatewayUpstreamEnv)); raw != "" {
		cfg.Upstream = raw
	}
	cfg.RedisURL = strings.TrimSpace(os.Getenv(redisURLEnv))
	cfg.JWTSecret = strings.TrimSpace(os.Getenv(jwtSecretEnv))

	rlCfg, err := ratelimit.LoadConfigFromEnv()
	if err != nil {
		return Config{}, fmt.Errorf("ratelimit config: %w", err)
	}
	cfg.RateLimit = rlCfg

	if raw := strings.TrimSpace(os.Getenv(ipModeEnv)); raw != "" {
		cfg.IPMode = IPMode(raw)
	}

	if raw := strings.TrimSpace(os.Getenv(trustedCIDRsEnv)); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			if s := strings.TrimSpace(part); s != "" {
				cfg.TrustedCIDRs = append(cfg.TrustedCIDRs, s)
			}
		}
	}

	cfg.GeoIPDBPath = strings.TrimSpace(os.Getenv(geoIPDBPathEnv))
	cfg.GeoIPLicenseKey = strings.TrimSpace(os.Getenv(geoIPLicenseKeyEnv))

	// 有 license key 但没指定 db path 时，使用默认路径
	if cfg.GeoIPLicenseKey != "" && cfg.GeoIPDBPath == "" {
		cfg.GeoIPDBPath = filepath.Join(defaultGeoIPDBDir, "GeoLite2-City.mmdb")
	}

	if raw := strings.TrimSpace(os.Getenv(riskRejectThresholdEnv)); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 0 || v > 100 {
			return Config{}, fmt.Errorf("%s: must be an integer 0-100", riskRejectThresholdEnv)
		}
		cfg.RiskRejectThreshold = v
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Addr) == "" {
		return fmt.Errorf("addr must not be empty")
	}
	if _, err := net.ResolveTCPAddr("tcp", c.Addr); err != nil {
		return fmt.Errorf("addr invalid: %w", err)
	}

	if strings.TrimSpace(c.Upstream) == "" {
		return fmt.Errorf("upstream must not be empty")
	}
	u, err := url.Parse(c.Upstream)
	if err != nil || strings.TrimSpace(u.Host) == "" {
		return fmt.Errorf("upstream must be a valid URL with host: %s", c.Upstream)
	}

	switch c.IPMode {
	case "", IPModeDirect, IPModeCloudflare, IPModeTrustedProxy:
	default:
		return fmt.Errorf("ip_mode must be one of: direct, cloudflare, trusted_proxy")
	}

	return nil
}

package app

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"arkloop/services/gateway/internal/ratelimit"
	"arkloop/services/shared/stringutil"
)

const (
	gatewayAddrEnv            = "ARKLOOP_GATEWAY_ADDR"
	gatewayUpstreamEnv        = "ARKLOOP_GATEWAY_UPSTREAM"
	gatewayFrontendEnv        = "ARKLOOP_GATEWAY_FRONTEND_UPSTREAM"
	redisURLEnv               = "ARKLOOP_REDIS_URL"
	gatewayRedisURLEnv        = "ARKLOOP_GATEWAY_REDIS_URL"
	jwtSecretEnv              = "ARKLOOP_AUTH_JWT_SECRET"
	enableBenchzEnv           = "ARKLOOP_GATEWAY_ENABLE_BENCHZ"
	redisTimeoutMsEnv         = "ARKLOOP_GATEWAY_REDIS_TIMEOUT_MS"
	trustTraceIDEnv           = "ARKLOOP_GATEWAY_TRUST_INCOMING_TRACE_ID"
	corsAllowedOriginsEnv     = "ARKLOOP_GATEWAY_CORS_ALLOWED_ORIGINS"
	ipModeEnv                 = "ARKLOOP_GATEWAY_IP_MODE"
	trustedCIDRsEnv           = "ARKLOOP_GATEWAY_TRUSTED_CIDRS"
	geoIPDBPathEnv            = "ARKLOOP_GEOIP_DB_PATH"
	geoIPLicenseKeyEnv        = "ARKLOOP_GEOIP_LICENSE_KEY"
	riskRejectThresholdEnv    = "ARKLOOP_GATEWAY_RISK_REJECT_THRESHOLD"

	defaultAddr       = "0.0.0.0:19000"
	defaultUpstream          = "http://127.0.0.1:19001"
	defaultGeoIPDBDir = "/data/geoip"

	defaultRedisTimeoutMs = 150
)

var defaultCORSAllowedOrigins = []string{
	"http://localhost:19080",
	"http://localhost:19081",
	"http://localhost:19082",
}

type IPMode string

const (
	IPModeDirect       IPMode = "direct"
	IPModeCloudflare   IPMode = "cloudflare"
	IPModeTrustedProxy IPMode = "trusted_proxy"
)

type Config struct {
	Addr                 string
	Upstream             string
	FrontendUpstream     string
	RedisURL             string
	RedisTimeout         time.Duration
	JWTSecret            string
	RateLimit            ratelimit.Config
	EnableBenchz         bool
	TrustIncomingTraceID bool

	CORSAllowedOrigins  []string
	IPMode              IPMode
	TrustedCIDRs        []string
	GeoIPDBPath         string
	GeoIPLicenseKey     string
	RiskRejectThreshold int
}

func DefaultConfig() Config {
	return Config{
		Addr:              defaultAddr,
		Upstream:          defaultUpstream,
		RateLimit:         ratelimit.DefaultConfig(),
		RedisTimeout:       defaultRedisTimeoutMs * time.Millisecond,
		CORSAllowedOrigins: append([]string(nil), defaultCORSAllowedOrigins...),
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
	cfg.FrontendUpstream = strings.TrimSpace(os.Getenv(gatewayFrontendEnv))
	cfg.RedisURL = strings.TrimSpace(os.Getenv(gatewayRedisURLEnv))
	if cfg.RedisURL == "" {
		cfg.RedisURL = strings.TrimSpace(os.Getenv(redisURLEnv))
	}
	cfg.JWTSecret = strings.TrimSpace(os.Getenv(jwtSecretEnv))

	rlCfg, err := ratelimit.LoadConfigFromEnv()
	if err != nil {
		return Config{}, fmt.Errorf("ratelimit config: %w", err)
	}
	cfg.RateLimit = rlCfg

	if raw := strings.TrimSpace(os.Getenv(enableBenchzEnv)); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: must be a boolean", enableBenchzEnv)
		}
		cfg.EnableBenchz = v
	}

	if raw := strings.TrimSpace(os.Getenv(trustTraceIDEnv)); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: must be a boolean", trustTraceIDEnv)
		}
		cfg.TrustIncomingTraceID = v
	}

	if raw := strings.TrimSpace(os.Getenv(corsAllowedOriginsEnv)); raw != "" {
		cfg.CORSAllowedOrigins = stringutil.SplitCSV(raw)
	}
	if raw := strings.TrimSpace(os.Getenv(ipModeEnv)); raw != "" {
		cfg.IPMode = IPMode(raw)
	}
	if raw := strings.TrimSpace(os.Getenv(trustedCIDRsEnv)); raw != "" {
		cfg.TrustedCIDRs = stringutil.SplitCSV(raw)
	}

	cfg.GeoIPDBPath = strings.TrimSpace(os.Getenv(geoIPDBPathEnv))
	cfg.GeoIPLicenseKey = strings.TrimSpace(os.Getenv(geoIPLicenseKeyEnv))
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

	if raw := strings.TrimSpace(os.Getenv(redisTimeoutMsEnv)); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 0 {
			return Config{}, fmt.Errorf("%s: must be a non-negative integer", redisTimeoutMsEnv)
		}
		cfg.RedisTimeout = time.Duration(v) * time.Millisecond
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
	if err := validateUpstreamURL(c.Upstream, "upstream"); err != nil {
		return err
	}
	if strings.TrimSpace(c.FrontendUpstream) != "" {
		if err := validateUpstreamURL(c.FrontendUpstream, "frontend_upstream"); err != nil {
			return err
		}
	}

	switch c.IPMode {
	case "", IPModeDirect, IPModeCloudflare, IPModeTrustedProxy:
	default:
		return fmt.Errorf("ip_mode must be one of: direct, cloudflare, trusted_proxy")
	}

	for _, origin := range c.CORSAllowedOrigins {
		if origin == "*" {
			return fmt.Errorf("cors_allowed_origins must not contain *")
		}
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("invalid cors origin: %s", origin)
		}
	}

	return nil
}

func validateUpstreamURL(raw string, label string) error {
	u, err := url.Parse(raw)
	if err != nil || strings.TrimSpace(u.Host) == "" {
		return fmt.Errorf("%s must be a valid URL with host: %s", label, raw)
	}
	return nil
}



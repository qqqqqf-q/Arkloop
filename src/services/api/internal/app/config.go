package app

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"arkloop/services/api/internal/auth"
)

const (
	apiGoAddrEnv            = "ARKLOOP_API_GO_ADDR"
	databaseURLPrimaryEnv   = "ARKLOOP_DATABASE_URL"
	databaseURLFallbackEnv  = "DATABASE_URL"
	databaseDirectURLEnv    = "ARKLOOP_DATABASE_DIRECT_URL"
	trustIncomingTraceIDEnv = "ARKLOOP_TRUST_INCOMING_TRACE_ID"
	trustXForwardedForEnv   = "ARKLOOP_TRUST_X_FORWARDED_FOR"
	defaultAddr             = "127.0.0.1:8001"

	redisURLEnv                    = "ARKLOOP_REDIS_URL"
	gatewayRedisURLEnv             = "ARKLOOP_GATEWAY_REDIS_URL"
	maxConcurrentRunsPerOrgEnv     = "ARKLOOP_MAX_CONCURRENT_RUNS_PER_ORG"
	defaultMaxConcurrentRunsPerOrg = int64(10)

	s3EndpointEnv  = "ARKLOOP_S3_ENDPOINT"
	s3AccessKeyEnv = "ARKLOOP_S3_ACCESS_KEY"
	s3SecretKeyEnv = "ARKLOOP_S3_SECRET_KEY"
	s3BucketEnv    = "ARKLOOP_S3_BUCKET"
	s3RegionEnv    = "ARKLOOP_S3_REGION"

	sseHeartbeatSecondsEnv = "ARKLOOP_SSE_HEARTBEAT_SECONDS"
	sseBatchLimitEnv       = "ARKLOOP_SSE_BATCH_LIMIT"

	bootstrapPlatformAdminEnv = "ARKLOOP_BOOTSTRAP_PLATFORM_ADMIN"

	runTimeoutMinutesEnv     = "ARKLOOP_RUN_TIMEOUT_MINUTES"
	defaultRunTimeoutMinutes = 5

	runEventsRetentionMonthsEnv     = "ARKLOOP_RUN_EVENTS_RETENTION_MONTHS"
	defaultRunEventsRetentionMonths = 3

	emailFromEnv  = "ARKLOOP_EMAIL_FROM"
	appBaseURLEnv = "ARKLOOP_APP_BASE_URL"

	turnstileSecretKeyEnv   = "ARKLOOP_TURNSTILE_SECRET_KEY"
	turnstileSiteKeyEnv     = "ARKLOOP_TURNSTILE_SITE_KEY"
	turnstileAllowedHostEnv = "ARKLOOP_TURNSTILE_ALLOWED_HOST"

	defaultSSEHeartbeatSeconds = 15.0
	defaultSSEBatchLimit       = 500
)

type SSEConfig struct {
	HeartbeatSeconds float64
	BatchLimit       int
}

func defaultSSEConfig() SSEConfig {
	return SSEConfig{
		HeartbeatSeconds: defaultSSEHeartbeatSeconds,
		BatchLimit:       defaultSSEBatchLimit,
	}
}

type Config struct {
	Addr                 string
	DatabaseDSN          string
	DirectDatabaseDSN    string // SSE LISTEN/NOTIFY 专用直连，不走 PgBouncer
	TrustIncomingTraceID bool
	TrustXForwardedFor   bool
	Auth                 *auth.Config
	SSE                  SSEConfig

	RedisURL                string
	GatewayRedisURL         string
	MaxConcurrentRunsPerOrg int64

	S3Endpoint  string
	S3AccessKey string
	S3SecretKey string
	S3Bucket    string
	S3Region    string

	BootstrapPlatformAdmin   string
	RunTimeoutMinutes        int
	RunEventsRetentionMonths int
	EmailFrom                string
	AppBaseURL               string

	TurnstileSecretKey   string
	TurnstileSiteKey     string
	TurnstileAllowedHost string
}

func DefaultConfig() Config {
	return Config{
		Addr:                     defaultAddr,
		SSE:                      defaultSSEConfig(),
		MaxConcurrentRunsPerOrg:  defaultMaxConcurrentRunsPerOrg,
		RunTimeoutMinutes:        defaultRunTimeoutMinutes,
		RunEventsRetentionMonths: defaultRunEventsRetentionMonths,
	}
}

func LoadConfigFromEnv() (Config, error) {
	cfg := DefaultConfig()

	if raw := strings.TrimSpace(os.Getenv(apiGoAddrEnv)); raw != "" {
		cfg.Addr = raw
	} else if raw := strings.TrimSpace(os.Getenv("PORT")); raw != "" {
		port, err := parsePort(raw)
		if err != nil {
			return Config{}, fmt.Errorf("PORT: %w", err)
		}
		cfg.Addr = ":" + strconv.Itoa(port)
	}

	if raw, ok := lookupEnv(trustIncomingTraceIDEnv); ok {
		enabled, err := parseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", trustIncomingTraceIDEnv, err)
		}
		cfg.TrustIncomingTraceID = enabled
	}

	if raw, ok := lookupEnv(trustXForwardedForEnv); ok {
		enabled, err := parseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", trustXForwardedForEnv, err)
		}
		cfg.TrustXForwardedFor = enabled
	}

	if raw, ok := lookupEnv(databaseURLPrimaryEnv); ok {
		cfg.DatabaseDSN = raw
	} else if raw, ok := lookupEnv(databaseURLFallbackEnv); ok {
		cfg.DatabaseDSN = raw
	}

	if raw, ok := lookupEnv(databaseDirectURLEnv); ok {
		cfg.DirectDatabaseDSN = raw
	}

	authConfig, err := auth.LoadConfigFromEnv(false)
	if err != nil {
		return Config{}, err
	}
	cfg.Auth = authConfig

	if raw, ok := lookupEnv(redisURLEnv); ok {
		cfg.RedisURL = raw
	}
	if raw, ok := lookupEnv(gatewayRedisURLEnv); ok {
		cfg.GatewayRedisURL = raw
	}
	if raw, ok := lookupEnv(maxConcurrentRunsPerOrgEnv); ok {
		v, err := parsePositiveInt64(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", maxConcurrentRunsPerOrgEnv, err)
		}
		cfg.MaxConcurrentRunsPerOrg = v
	}
	if raw, ok := lookupEnv(s3EndpointEnv); ok {
		cfg.S3Endpoint = raw
	}
	if raw, ok := lookupEnv(s3AccessKeyEnv); ok {
		cfg.S3AccessKey = raw
	}
	if raw, ok := lookupEnv(s3SecretKeyEnv); ok {
		cfg.S3SecretKey = raw
	}
	if raw, ok := lookupEnv(s3BucketEnv); ok {
		cfg.S3Bucket = raw
	}
	if raw, ok := lookupEnv(s3RegionEnv); ok {
		cfg.S3Region = raw
	}

	if raw, ok := lookupEnv(sseHeartbeatSecondsEnv); ok {
		v, err := parseNonNegativeFloat(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", sseHeartbeatSecondsEnv, err)
		}
		cfg.SSE.HeartbeatSeconds = v
	}
	if raw, ok := lookupEnv(sseBatchLimitEnv); ok {
		v, err := parsePositiveInt(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", sseBatchLimitEnv, err)
		}
		cfg.SSE.BatchLimit = v
	}

	if raw, ok := lookupEnv(bootstrapPlatformAdminEnv); ok {
		cfg.BootstrapPlatformAdmin = raw
	}

	if raw, ok := lookupEnv(runTimeoutMinutesEnv); ok {
		v, err := parsePositiveInt(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", runTimeoutMinutesEnv, err)
		}
		cfg.RunTimeoutMinutes = v
	}

	if raw, ok := lookupEnv(runEventsRetentionMonthsEnv); ok {
		v, err := parsePositiveInt(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", runEventsRetentionMonthsEnv, err)
		}
		cfg.RunEventsRetentionMonths = v
	}

	if raw, ok := lookupEnv(emailFromEnv); ok {
		cfg.EmailFrom = raw
	}

	if raw, ok := lookupEnv(appBaseURLEnv); ok {
		cfg.AppBaseURL = raw
	}

	if raw, ok := lookupEnv(turnstileSecretKeyEnv); ok {
		cfg.TurnstileSecretKey = raw
	}
	if raw, ok := lookupEnv(turnstileSiteKeyEnv); ok {
		cfg.TurnstileSiteKey = raw
	}
	if raw, ok := lookupEnv(turnstileAllowedHostEnv); ok {
		cfg.TurnstileAllowedHost = raw
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	addr := strings.TrimSpace(c.Addr)
	if addr == "" {
		return fmt.Errorf("addr must not be empty")
	}
	if _, err := net.ResolveTCPAddr("tcp", addr); err != nil {
		return fmt.Errorf("addr invalid: %w", err)
	}

	if c.Auth != nil {
		if err := c.Auth.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func lookupEnv(key string) (string, bool) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return "", false
	}
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return "", false
	}
	return cleaned, true
}

func parsePort(raw string) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("must be an integer")
	}
	if value <= 0 || value > 65535 {
		return 0, fmt.Errorf("must be in range 1-65535")
	}
	return value, nil
}

func parseNonNegativeFloat(raw string) (float64, error) {
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, fmt.Errorf("must be a number")
	}
	if v < 0 {
		return 0, fmt.Errorf("must be non-negative")
	}
	return v, nil
}

func parsePositiveInt64(raw string) (int64, error) {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("must be an integer")
	}
	if value <= 0 {
		return 0, fmt.Errorf("must be greater than 0")
	}
	return value, nil
}

func parsePositiveInt(raw string) (int, error) {
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("must be an integer")
	}
	if v <= 0 {
		return 0, fmt.Errorf("must be a positive integer")
	}
	return v, nil
}

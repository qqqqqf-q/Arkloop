package app

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"arkloop/services/api_go/internal/auth"
)

const (
	apiGoAddrEnv            = "ARKLOOP_API_GO_ADDR"
	databaseURLPrimaryEnv   = "ARKLOOP_DATABASE_URL"
	databaseURLFallbackEnv  = "DATABASE_URL"
	trustIncomingTraceIDEnv = "ARKLOOP_TRUST_INCOMING_TRACE_ID"
	defaultAddr             = "127.0.0.1:8001"

	ssePollSecondsEnv      = "ARKLOOP_SSE_POLL_SECONDS"
	sseHeartbeatSecondsEnv = "ARKLOOP_SSE_HEARTBEAT_SECONDS"
	sseBatchLimitEnv       = "ARKLOOP_SSE_BATCH_LIMIT"

	defaultSSEPollSeconds      = 0.25
	defaultSSEHeartbeatSeconds = 15.0
	defaultSSEBatchLimit       = 500
)

type SSEConfig struct {
	PollSeconds      float64
	HeartbeatSeconds float64
	BatchLimit       int
}

func defaultSSEConfig() SSEConfig {
	return SSEConfig{
		PollSeconds:      defaultSSEPollSeconds,
		HeartbeatSeconds: defaultSSEHeartbeatSeconds,
		BatchLimit:       defaultSSEBatchLimit,
	}
}

type Config struct {
	Addr                 string
	DatabaseDSN          string
	TrustIncomingTraceID bool
	Auth                 *auth.Config
	SSE                  SSEConfig
}

func DefaultConfig() Config {
	return Config{
		Addr: defaultAddr,
		SSE:  defaultSSEConfig(),
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

	if raw, ok := lookupEnv(databaseURLPrimaryEnv); ok {
		cfg.DatabaseDSN = raw
	} else if raw, ok := lookupEnv(databaseURLFallbackEnv); ok {
		cfg.DatabaseDSN = raw
	}

	authConfig, err := auth.LoadConfigFromEnv(false)
	if err != nil {
		return Config{}, err
	}
	cfg.Auth = authConfig

	if raw, ok := lookupEnv(ssePollSecondsEnv); ok {
		v, err := parseNonNegativeFloat(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", ssePollSecondsEnv, err)
		}
		cfg.SSE.PollSeconds = v
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

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	addr := strings.TrimSpace(c.Addr)
	if addr == "" {
		return fmt.Errorf("addr 不能为空")
	}
	if _, err := net.ResolveTCPAddr("tcp", addr); err != nil {
		return fmt.Errorf("addr 无效: %w", err)
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
		return 0, fmt.Errorf("必须为整数")
	}
	if value <= 0 || value > 65535 {
		return 0, fmt.Errorf("必须在 1-65535 之间")
	}
	return value, nil
}

func parseNonNegativeFloat(raw string) (float64, error) {
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, fmt.Errorf("必须为数字")
	}
	if v < 0 {
		return 0, fmt.Errorf("必须为非负数")
	}
	return v, nil
}

func parsePositiveInt(raw string) (int, error) {
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("必须为整数")
	}
	if v <= 0 {
		return 0, fmt.Errorf("必须为正整数")
	}
	return v, nil
}

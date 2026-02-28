package ratelimit

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	capacityEnv        = "ARKLOOP_RATELIMIT_CAPACITY"
	ratePerMinuteEnv   = "ARKLOOP_RATELIMIT_RATE_PER_MINUTE"
	defaultCapacity    = 600
	defaultPerMinute   = 300
)

type Config struct {
	// Capacity 是 token bucket 的最大容量（burst 上限）。
	Capacity float64
	// RatePerMinute 是每分钟填充的 token 数量。
	RatePerMinute float64
}

func (c Config) RatePerSecond() float64 {
	return c.RatePerMinute / 60.0
}

func DefaultConfig() Config {
	return Config{
		Capacity:      defaultCapacity,
		RatePerMinute: defaultPerMinute,
	}
}

func LoadConfigFromEnv() (Config, error) {
	cfg := DefaultConfig()

	if raw := strings.TrimSpace(os.Getenv(capacityEnv)); raw != "" {
		v, err := parsePositiveFloat(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", capacityEnv, err)
		}
		cfg.Capacity = v
	}

	if raw := strings.TrimSpace(os.Getenv(ratePerMinuteEnv)); raw != "" {
		v, err := parsePositiveFloat(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", ratePerMinuteEnv, err)
		}
		cfg.RatePerMinute = v
	}

	return cfg, nil
}

func parsePositiveFloat(raw string) (float64, error) {
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("must be a positive number")
	}
	return v, nil
}

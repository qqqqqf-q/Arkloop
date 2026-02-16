package app

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	workerConcurrencyEnv      = "ARKLOOP_WORKER_CONCURRENCY"
	workerPollSecondsEnv      = "ARKLOOP_WORKER_POLL_SECONDS"
	workerLeaseSecondsEnv     = "ARKLOOP_WORKER_LEASE_SECONDS"
	workerHeartbeatSecondsEnv = "ARKLOOP_WORKER_HEARTBEAT_SECONDS"
	workerQueueJobTypesEnv    = "ARKLOOP_WORKER_QUEUE_JOB_TYPES"
)

// Config 与 Python WorkerLoopConfig 对齐。
type Config struct {
	Concurrency      int
	PollSeconds      float64
	LeaseSeconds     int
	HeartbeatSeconds float64
	QueueJobTypes    []string
}

func DefaultConfig() Config {
	return Config{
		Concurrency:      4,
		PollSeconds:      0.25,
		LeaseSeconds:     30,
		HeartbeatSeconds: 10,
		QueueJobTypes:    []string{"run.execute"},
	}
}

func LoadConfigFromEnv() (Config, error) {
	cfg := DefaultConfig()

	if raw, ok := lookupEnv(workerConcurrencyEnv); ok {
		value, err := parsePositiveInt(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", workerConcurrencyEnv, err)
		}
		cfg.Concurrency = value
	}

	if raw, ok := lookupEnv(workerPollSecondsEnv); ok {
		value, err := parseNonNegativeFloat(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", workerPollSecondsEnv, err)
		}
		cfg.PollSeconds = value
	}

	if raw, ok := lookupEnv(workerLeaseSecondsEnv); ok {
		value, err := parsePositiveInt(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", workerLeaseSecondsEnv, err)
		}
		cfg.LeaseSeconds = value
	}

	if raw, ok := lookupEnv(workerHeartbeatSecondsEnv); ok {
		value, err := parseNonNegativeFloat(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", workerHeartbeatSecondsEnv, err)
		}
		cfg.HeartbeatSeconds = value
	}

	if raw, ok := lookupEnv(workerQueueJobTypesEnv); ok {
		parsed := parseCSVList(raw)
		if len(parsed) == 0 {
			return Config{}, fmt.Errorf("%s: 不能为空", workerQueueJobTypesEnv)
		}
		cfg.QueueJobTypes = parsed
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (c Config) Validate() error {
	if c.Concurrency <= 0 {
		return fmt.Errorf("concurrency 必须为正整数")
	}
	if c.PollSeconds < 0 {
		return fmt.Errorf("poll_seconds 必须为非负数")
	}
	if c.LeaseSeconds <= 0 {
		return fmt.Errorf("lease_seconds 必须为正整数")
	}
	if c.HeartbeatSeconds < 0 {
		return fmt.Errorf("heartbeat_seconds 必须为非负数")
	}
	if len(c.QueueJobTypes) == 0 {
		return fmt.Errorf("queue_job_types 不能为空")
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

func parsePositiveInt(raw string) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("必须为整数")
	}
	if value <= 0 {
		return 0, fmt.Errorf("必须大于 0")
	}
	return value, nil
}

func parseNonNegativeFloat(raw string) (float64, error) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, fmt.Errorf("必须为浮点数")
	}
	if value < 0 {
		return 0, fmt.Errorf("必须大于等于 0")
	}
	return value, nil
}

func parseCSVList(raw string) []string {
	items := strings.Split(raw, ",")
	seen := make(map[string]struct{}, len(items))
	deduped := make([]string, 0, len(items))
	for _, item := range items {
		cleaned := strings.TrimSpace(item)
		if cleaned == "" {
			continue
		}
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		deduped = append(deduped, cleaned)
	}
	return deduped
}

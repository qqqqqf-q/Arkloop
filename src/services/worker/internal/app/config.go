package app

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"arkloop/services/worker/internal/queue"
)

const (
	workerConcurrencyEnv      = "ARKLOOP_WORKER_CONCURRENCY"
	workerPollSecondsEnv      = "ARKLOOP_WORKER_POLL_SECONDS"
	workerLeaseSecondsEnv     = "ARKLOOP_WORKER_LEASE_SECONDS"
	workerHeartbeatSecondsEnv = "ARKLOOP_WORKER_HEARTBEAT_SECONDS"
	workerQueueJobTypesEnv    = "ARKLOOP_WORKER_QUEUE_JOB_TYPES"
	workerCapabilitiesEnv     = "ARKLOOP_WORKER_CAPABILITIES"
	workerVersionEnv          = "ARKLOOP_WORKER_VERSION"
)

// Config aligns with worker loop behavior.
type Config struct {
	Concurrency      int
	PollSeconds      float64
	LeaseSeconds     int
	HeartbeatSeconds float64
	QueueJobTypes    []string
	Capabilities     []string
	Version          string
}

func DefaultConfig() Config {
	return Config{
		Concurrency:      4,
		PollSeconds:      0.25,
		LeaseSeconds:     30,
		HeartbeatSeconds: 10,
		QueueJobTypes:    []string{queue.RunExecuteJobType, queue.WebhookDeliverJobType},
		Capabilities:     []string{queue.RunExecuteJobType, queue.WebhookDeliverJobType},
		Version:          "unknown",
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
			return Config{}, fmt.Errorf("%s: must not be empty", workerQueueJobTypesEnv)
		}
		cfg.QueueJobTypes = parsed
	}

	if raw, ok := lookupEnv(workerCapabilitiesEnv); ok {
		cfg.Capabilities = parseCSVList(raw)
	}

	if raw, ok := lookupEnv(workerVersionEnv); ok {
		cfg.Version = raw
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (c Config) Validate() error {
	if c.Concurrency <= 0 {
		return fmt.Errorf("concurrency must be a positive integer")
	}
	if c.PollSeconds < 0 {
		return fmt.Errorf("poll_seconds must be non-negative")
	}
	if c.LeaseSeconds <= 0 {
		return fmt.Errorf("lease_seconds must be a positive integer")
	}
	if c.HeartbeatSeconds < 0 {
		return fmt.Errorf("heartbeat_seconds must be non-negative")
	}
	if len(c.QueueJobTypes) == 0 {
		return fmt.Errorf("queue_job_types must not be empty")
	}
	supported := map[string]struct{}{
		queue.RunExecuteJobType:     {},
		queue.WebhookDeliverJobType: {},
	}
	for _, jobType := range c.QueueJobTypes {
		if _, ok := supported[jobType]; !ok {
			return fmt.Errorf("unsupported job type: %s", jobType)
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

func parsePositiveInt(raw string) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("must be an integer")
	}
	if value <= 0 {
		return 0, fmt.Errorf("must be greater than 0")
	}
	return value, nil
}

func parseNonNegativeFloat(raw string) (float64, error) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, fmt.Errorf("must be a float")
	}
	if value < 0 {
		return 0, fmt.Errorf("must be greater than or equal to 0")
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

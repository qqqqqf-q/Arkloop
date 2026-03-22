package app

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"arkloop/services/worker/internal/queue"
)

const (
	workerConcurrencyEnv           = "ARKLOOP_WORKER_CONCURRENCY"
	workerPollSecondsEnv           = "ARKLOOP_WORKER_POLL_SECONDS"
	workerLeaseSecondsEnv          = "ARKLOOP_WORKER_LEASE_SECONDS"
	workerHeartbeatSecondsEnv      = "ARKLOOP_WORKER_HEARTBEAT_SECONDS"
	workerQueueJobTypesEnv         = "ARKLOOP_WORKER_QUEUE_JOB_TYPES"
	workerCapabilitiesEnv          = "ARKLOOP_WORKER_CAPABILITIES"
	workerVersionEnv               = "ARKLOOP_WORKER_VERSION"
	mcpCacheTTLSecondsEnv          = "ARKLOOP_MCP_CACHE_TTL_SECONDS"
	toolProviderCacheTTLSecondsEnv = "ARKLOOP_TOOL_PROVIDER_CACHE_TTL_SECONDS"
	queueDriverEnv                 = "ARKLOOP_QUEUE_DRIVER"

	workerMinConcurrencyEnv     = "ARKLOOP_WORKER_MIN_CONCURRENCY"
	workerMaxConcurrencyEnv     = "ARKLOOP_WORKER_MAX_CONCURRENCY"
	workerScaleUpThresholdEnv   = "ARKLOOP_WORKER_SCALE_UP_THRESHOLD"
	workerScaleDownThresholdEnv = "ARKLOOP_WORKER_SCALE_DOWN_THRESHOLD"
	workerScaleIntervalSecsEnv  = "ARKLOOP_WORKER_SCALE_INTERVAL_SECS"
	workerScaleCooldownSecsEnv  = "ARKLOOP_WORKER_SCALE_COOLDOWN_SECS"
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

	// MCP 发现结果缓存 TTL（秒），0 表示不缓存
	MCPCacheTTLSeconds int

	// Tool Provider 配置缓存 TTL（秒），0 表示不缓存
	ToolProviderCacheTTLSeconds int

	// QueueDriver selects the job-queue implementation: "pg" (default) or "channel".
	QueueDriver string

	// Adaptive scaling
	MinConcurrency     int
	MaxConcurrency     int
	ScaleUpThreshold   int
	ScaleDownThreshold int
	ScaleIntervalSecs  float64
	ScaleCooldownSecs  float64
}

func DefaultConfig() Config {
	return Config{
		Concurrency:                 4,
		PollSeconds:                 5,
		LeaseSeconds:                30,
		HeartbeatSeconds:            10,
		QueueJobTypes:               []string{queue.RunExecuteJobType, queue.WebhookDeliverJobType, queue.EmailSendJobType},
		Capabilities:                []string{queue.RunExecuteJobType, queue.WebhookDeliverJobType, queue.EmailSendJobType},
		Version:                     "unknown",
		MCPCacheTTLSeconds:          60,
		ToolProviderCacheTTLSeconds: 60,
		QueueDriver:                 "pg",
		MinConcurrency:              2,
		MaxConcurrency:              16,
		ScaleUpThreshold:            3,
		ScaleDownThreshold:          1,
		ScaleIntervalSecs:           5,
		ScaleCooldownSecs:           30,
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

	if raw, ok := lookupEnv(mcpCacheTTLSecondsEnv); ok {
		value, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			return Config{}, fmt.Errorf("%s: must be an integer", mcpCacheTTLSecondsEnv)
		}
		if value < 0 {
			return Config{}, fmt.Errorf("%s: must be >= 0", mcpCacheTTLSecondsEnv)
		}
		cfg.MCPCacheTTLSeconds = value
	}

	if raw, ok := lookupEnv(queueDriverEnv); ok {
		cfg.QueueDriver = raw
	}

	if raw, ok := lookupEnv(workerMinConcurrencyEnv); ok {
		value, err := parsePositiveInt(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", workerMinConcurrencyEnv, err)
		}
		cfg.MinConcurrency = value
	}

	if raw, ok := lookupEnv(workerMaxConcurrencyEnv); ok {
		value, err := parsePositiveInt(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", workerMaxConcurrencyEnv, err)
		}
		cfg.MaxConcurrency = value
	}

	if raw, ok := lookupEnv(workerScaleUpThresholdEnv); ok {
		value, err := parsePositiveInt(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", workerScaleUpThresholdEnv, err)
		}
		cfg.ScaleUpThreshold = value
	}

	if raw, ok := lookupEnv(workerScaleDownThresholdEnv); ok {
		value, err := parseNonNegativeInt(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", workerScaleDownThresholdEnv, err)
		}
		cfg.ScaleDownThreshold = value
	}

	if raw, ok := lookupEnv(workerScaleIntervalSecsEnv); ok {
		value, err := parsePositiveFloat(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", workerScaleIntervalSecsEnv, err)
		}
		cfg.ScaleIntervalSecs = value
	}

	if raw, ok := lookupEnv(workerScaleCooldownSecsEnv); ok {
		value, err := parseNonNegativeFloat(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", workerScaleCooldownSecsEnv, err)
		}
		cfg.ScaleCooldownSecs = value
	}

	if raw, ok := lookupEnv(toolProviderCacheTTLSecondsEnv); ok {
		value, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			return Config{}, fmt.Errorf("%s: must be an integer", toolProviderCacheTTLSecondsEnv)
		}
		if value < 0 {
			return Config{}, fmt.Errorf("%s: must be >= 0", toolProviderCacheTTLSecondsEnv)
		}
		cfg.ToolProviderCacheTTLSeconds = value
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
		queue.EmailSendJobType:      {},
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

func parsePositiveFloat(raw string) (float64, error) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, fmt.Errorf("must be a float")
	}
	if value <= 0 {
		return 0, fmt.Errorf("must be greater than 0")
	}
	return value, nil
}

func parseNonNegativeInt(raw string) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("must be an integer")
	}
	if value < 0 {
		return 0, fmt.Errorf("must be >= 0")
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

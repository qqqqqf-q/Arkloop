package data

// ThreadConfig 从 threads.config_json 中解析的 thread 级配置。
type ThreadConfig struct {
	DefaultModel            string `json:"default_model,omitempty"`
	ReasoningMode           string `json:"reasoning_mode,omitempty"`
	HeartbeatEnabled        *bool  `json:"heartbeat_enabled,omitempty"`
	HeartbeatIntervalMinute int    `json:"heartbeat_interval_minutes,omitempty"`
	HeartbeatModel          string `json:"heartbeat_model,omitempty"`
}

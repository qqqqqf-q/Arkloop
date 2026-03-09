package environment

import "time"

type Config struct {
	DebounceDelay       time.Duration
	MaxDirtyAge         time.Duration
	ForceBytesThreshold int64
	ForceCountThreshold int
	LeaseTTL            time.Duration
	GovernanceInterval  time.Duration
}

func DefaultConfig() Config {
	return Config{
		DebounceDelay:       2 * time.Second,
		MaxDirtyAge:         15 * time.Second,
		ForceBytesThreshold: 16 << 20,
		ForceCountThreshold: 512,
		LeaseTTL:            3 * time.Minute,
		GovernanceInterval:  time.Minute,
	}
}

func normalizeConfig(cfg Config) Config {
	defaults := DefaultConfig()
	if cfg.DebounceDelay <= 0 {
		cfg.DebounceDelay = defaults.DebounceDelay
	}
	if cfg.MaxDirtyAge <= 0 {
		cfg.MaxDirtyAge = defaults.MaxDirtyAge
	}
	if cfg.ForceBytesThreshold <= 0 {
		cfg.ForceBytesThreshold = defaults.ForceBytesThreshold
	}
	if cfg.ForceCountThreshold <= 0 {
		cfg.ForceCountThreshold = defaults.ForceCountThreshold
	}
	if cfg.LeaseTTL <= 0 {
		cfg.LeaseTTL = defaults.LeaseTTL
	}
	if cfg.GovernanceInterval <= 0 {
		cfg.GovernanceInterval = defaults.GovernanceInterval
	}
	return cfg
}

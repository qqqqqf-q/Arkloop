package consumer

import "fmt"

type Config struct {
	Concurrency      int
	PollSeconds      float64
	LeaseSeconds     int
	HeartbeatSeconds float64
	QueueJobTypes    []string

	// Adaptive scaling
	MinConcurrency     int
	MaxConcurrency     int
	ScaleUpThreshold   int     // queue depth per worker to trigger scale-up
	ScaleDownThreshold int     // queue depth per worker to trigger scale-down
	ScaleIntervalSecs  float64 // how often to evaluate scaling
	ScaleCooldownSecs  float64 // min time between scale decisions
}

func DefaultConfig() Config {
	return Config{
		Concurrency:        4,
		PollSeconds:        5,
		LeaseSeconds:       30,
		HeartbeatSeconds:   10,
		QueueJobTypes:      []string{"run.execute"},
		MinConcurrency:     2,
		MaxConcurrency:     16,
		ScaleUpThreshold:   3,
		ScaleDownThreshold: 1,
		ScaleIntervalSecs:  5,
		ScaleCooldownSecs:  30,
	}
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
	return nil
}

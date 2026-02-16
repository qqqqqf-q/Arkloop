package consumer

import "fmt"

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

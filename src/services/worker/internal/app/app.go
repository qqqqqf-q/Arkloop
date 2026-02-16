package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

type Application struct {
	config Config
	logger *JSONLogger
}

func NewApplication(config Config, logger *JSONLogger) (*Application, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		return nil, fmt.Errorf("logger 不能为空")
	}
	return &Application{
		config: config,
		logger: logger,
	}, nil
}

func (a *Application) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	a.logger.Info("worker_go 已启动（WG01 骨架，不消费任务）", LogFields{}, map[string]any{
		"concurrency":       a.config.Concurrency,
		"poll_seconds":      a.config.PollSeconds,
		"lease_seconds":     a.config.LeaseSeconds,
		"heartbeat_seconds": a.config.HeartbeatSeconds,
		"queue_job_types":   a.config.QueueJobTypes,
	})

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)

	select {
	case <-ctx.Done():
		a.logger.Info("worker_go 收到停止信号", LogFields{}, map[string]any{"reason": "context_cancelled"})
		return nil
	case sig := <-signals:
		a.logger.Info("worker_go 收到停止信号", LogFields{}, map[string]any{
			"reason": "os_signal",
			"signal": sig.String(),
		})
		return nil
	}
}

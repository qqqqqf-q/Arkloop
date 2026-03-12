//go:build desktop

package main

import (
	"context"
	"os"

	"arkloop/services/shared/eventbus"
	"arkloop/services/worker/internal/app"
	"arkloop/services/worker/internal/consumer"
	"arkloop/services/worker/internal/queue"
)

// run is the desktop entry point. It uses in-process adapters (LocalEventBus,
// ChannelJobQueue) and does not depend on PostgreSQL, Redis, or S3.
func run() error {
	if _, err := app.LoadDotenvIfEnabled(false); err != nil {
		return err
	}

	cfg, err := app.LoadConfigFromEnv()
	if err != nil {
		return err
	}

	logger := app.NewJSONLogger("worker_go", os.Stdout)

	bus := eventbus.NewLocalEventBus()

	localNotifier := consumer.NewLocalNotifier()
	cq, err := queue.NewChannelJobQueue(25, localNotifier.Notify)
	if err != nil {
		return err
	}

	logger.Info("desktop mode: using LocalEventBus and ChannelJobQueue", app.LogFields{}, nil)
	_ = bus
	_ = cq

	application, err := app.NewApplication(cfg, logger)
	if err != nil {
		return err
	}
	return application.Run(context.Background())
}

//go:build darwin && !desktop

package main

import (
	"arkloop/services/sandbox/internal/app"
	"arkloop/services/sandbox/internal/logging"
	"arkloop/services/sandbox/internal/session"
	vzpool "arkloop/services/sandbox/internal/vz"
)

func buildVzPool(cfg app.Config, logger *logging.JSONLogger) (session.Provider, error) {
	pool := vzpool.New(vzpool.Config{
		WarmSizes:             cfg.WarmSizes(),
		RefillIntervalSeconds: cfg.RefillIntervalSeconds,
		MaxRefillConcurrency:  cfg.RefillConcurrency,
		KernelImagePath:       cfg.KernelImagePath,
		RootfsPath:            cfg.RootfsPath,
		SocketBaseDir:         cfg.SocketBaseDir,
		BootTimeoutSeconds:    cfg.BootTimeoutSeconds,
		GuestAgentPort:        cfg.GuestAgentPort,
		Logger:                logger,
	})
	pool.Start()
	return pool, nil
}

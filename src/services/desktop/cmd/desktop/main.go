//go:build desktop

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	api "arkloop/services/api"
	bridge "arkloop/services/bridge"
	"arkloop/services/shared/desktop"
	worker "arkloop/services/worker"
)

func main() {
	if err := run(); err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 1. 提前初始化共享 queue 和 event bus，注入到 desktop 全局状态。
	//    Worker.InitDesktopInfra 创建 ChannelJobQueue + LocalEventBus
	//    并通过 desktop.Set* 注册，不打开 SQLite。
	if err := worker.InitDesktopInfra(); err != nil {
		return fmt.Errorf("init infra: %w", err)
	}

	// 2. API 先启动：打开 SQLite → 执行 migration → seed → HTTP server。
	//    这样 migration 在 Worker 使用 db 之前完成，不会冲突。
	apiErr := make(chan error, 1)
	go func() {
		apiErr <- api.StartDesktop(ctx)
	}()

	// 3. 等待 API 完成初始化（migration + HTTP server 启动）
	waitCtx, waitCancel := context.WithTimeout(ctx, 30*time.Second)
	defer waitCancel()

	// 同时监听 apiErr，避免 API 初始化失败时永远等待
	apiReadyCh := make(chan error, 1)
	go func() {
		apiReadyCh <- desktop.WaitAPIReady(waitCtx)
	}()

	select {
	case err := <-apiReadyCh:
		if err != nil {
			return fmt.Errorf("api init: %w", err)
		}
	case err := <-apiErr:
		return fmt.Errorf("api failed during init: %w", err)
	}

	// 4. Worker 启动：打开同一个 SQLite（migration 已完成），开始消费。
	workerErr := make(chan error, 1)
	go func() {
		workerErr <- worker.StartDesktop(ctx)
	}()

	// 5. Bridge service (best-effort, does not block API/Worker).
	go func() {
		if err := bridge.StartDesktop(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "bridge: %v\n", err)
		}
	}()

	select {
	case err := <-apiErr:
		if err != nil {
			fmt.Fprintf(os.Stderr, "api: %v\n", err)
		}
	case err := <-workerErr:
		if err != nil {
			fmt.Fprintf(os.Stderr, "worker: %v\n", err)
		}
	case <-ctx.Done():
	}

	stop()

	graceful := time.After(5 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case <-apiErr:
		case <-workerErr:
		case <-graceful:
			return nil
		}
	}
	return nil
}

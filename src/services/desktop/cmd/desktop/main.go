//go:build desktop

package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	api "arkloop/services/api"
	bridge "arkloop/services/bridge"
	desktopsandbox "arkloop/services/sandbox/desktopserver"
	"arkloop/services/shared/desktop"
	worker "arkloop/services/worker"
)

func main() {
	if err := run(); err != nil {
		slog.Error("desktop main error", "err", err)
		os.Exit(1)
	}
}

func run() error {
	baseCtx := context.Background()
	apiCtx, cancelAPI := context.WithCancel(baseCtx)
	workerCtx, cancelWorker := context.WithCancel(baseCtx)

	if err := worker.InitDesktopInfra(); err != nil {
		cancelAPI()
		cancelWorker()
		return err
	}
	desktop.RestoreExecutionModeFromDisk()
	desktop.SetSidecarProcess(true)
	defer func() {
		if err := desktop.CloseRegisteredSQLite(); err != nil {
			slog.Error("sqlite close", "err", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	apiErr := make(chan error, 1)
	go func() {
		apiErr <- api.StartDesktop(apiCtx)
	}()

	waitCtx, waitCancel := context.WithTimeout(apiCtx, 30*time.Second)
	defer waitCancel()

	apiReadyCh := make(chan error, 1)
	go func() {
		apiReadyCh <- desktop.WaitAPIReady(waitCtx)
	}()

	select {
	case err := <-apiReadyCh:
		if err != nil {
			cancelAPI()
			cancelWorker()
			return err
		}
	case err := <-apiErr:
		cancelAPI()
		cancelWorker()
		return err
	}

	// Always start the embedded sandbox so sandboxAddr is populated;
	// if kernel/rootfs are not configured it falls back to trusted mode gracefully.
	startEmbeddedSandbox(apiCtx)

	workerErr := make(chan error, 1)
	go func() {
		workerErr <- worker.StartDesktop(workerCtx)
	}()

	go func() {
		if err := bridge.StartDesktop(apiCtx); err != nil {
			slog.Error("bridge error", "err", err)
		}
	}()

	var firstErr error
	select {
	case err := <-apiErr:
		if err != nil {
			slog.Error("api error", "err", err)
			firstErr = err
		}
		cancelWorker()
		if werr := <-workerErr; werr != nil {
			slog.Error("worker error", "err", werr)
			if firstErr == nil {
				firstErr = werr
			}
		}
		cancelAPI()
	case err := <-workerErr:
		if err != nil {
			slog.Error("worker error", "err", err)
			firstErr = err
		}
		cancelWorker()
		cancelAPI()
		if aerr := <-apiErr; aerr != nil {
			slog.Error("api error", "err", aerr)
			if firstErr == nil {
				firstErr = aerr
			}
		}
	case <-sigCh:
		cancelWorker()
		if werr := <-workerErr; werr != nil {
			slog.Error("worker error", "err", werr)
			firstErr = werr
		}
		cancelAPI()
		if aerr := <-apiErr; aerr != nil {
			slog.Error("api error", "err", aerr)
			if firstErr == nil {
				firstErr = aerr
			}
		}
	}

	return firstErr
}

// startEmbeddedSandbox creates and starts a lightweight VZ sandbox HTTP
// server inside the sidecar process. On failure it logs a warning and
// falls back to trusted local mode.
func startEmbeddedSandbox(ctx context.Context) {
	kernelPath := strings.TrimSpace(os.Getenv("ARKLOOP_SANDBOX_KERNEL_IMAGE"))
	rootfsPath := strings.TrimSpace(os.Getenv("ARKLOOP_SANDBOX_ROOTFS"))
	initrdPath := strings.TrimSpace(os.Getenv("ARKLOOP_SANDBOX_INITRD"))
	socketDir := strings.TrimSpace(os.Getenv("ARKLOOP_SANDBOX_SOCKET_DIR"))

	if kernelPath == "" || rootfsPath == "" {
		slog.Warn("sandbox: kernel/rootfs paths not configured, falling back to trusted mode")
		return
	}

	if _, err := os.Stat(kernelPath); err != nil {
		slog.Warn("sandbox: kernel not found, falling back to trusted mode", "path", kernelPath)
		return
	}
	if _, err := os.Stat(rootfsPath); err != nil {
		slog.Warn("sandbox: rootfs not found, falling back to trusted mode", "path", rootfsPath)
		return
	}
	if initrdPath != "" {
		if _, err := os.Stat(initrdPath); err != nil {
			slog.Warn("sandbox: initrd not found, proceeding without initrd", "path", initrdPath)
			initrdPath = ""
		}
	}

	if socketDir == "" {
		home, _ := os.UserHomeDir()
		socketDir = home + "/.arkloop/vm/sessions"
	}

	cfg := desktopsandbox.Config{
		ListenAddr:     "127.0.0.1:0",
		KernelImage:    kernelPath,
		InitrdPath:     initrdPath,
		RootfsPath:     rootfsPath,
		SocketBaseDir:  socketDir,
		BootTimeout:    60,
		GuestAgentPort: 8080,
		AuthToken:      strings.TrimSpace(os.Getenv("ARKLOOP_DESKTOP_TOKEN")),
	}

	srv, err := desktopsandbox.New(cfg)
	if err != nil {
		slog.Warn("sandbox: init failed, falling back to trusted mode", "err", err)
		return
	}

	addr, err := srv.Start(ctx)
	if err != nil {
		slog.Warn("sandbox: start failed, falling back to trusted mode", "err", err)
		return
	}

	desktop.SetSandboxAddr(addr)
	slog.Info("sandbox: embedded VZ sandbox listening", "addr", addr)
}

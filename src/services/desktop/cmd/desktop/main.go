//go:build desktop

package main

import (
	"context"
	"fmt"
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
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
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
		return fmt.Errorf("init infra: %w", err)
	}
	desktop.RestoreExecutionModeFromDisk()
	desktop.SetSidecarProcess(true)
	defer func() {
		if err := desktop.CloseRegisteredSQLite(); err != nil {
			fmt.Fprintf(os.Stderr, "sqlite close: %v\n", err)
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
			return fmt.Errorf("api init: %w", err)
		}
	case err := <-apiErr:
		cancelAPI()
		cancelWorker()
		return fmt.Errorf("api failed during init: %w", err)
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
			fmt.Fprintf(os.Stderr, "bridge: %v\n", err)
		}
	}()

	var firstErr error
	select {
	case err := <-apiErr:
		if err != nil {
			fmt.Fprintf(os.Stderr, "api: %v\n", err)
			firstErr = err
		}
		cancelWorker()
		if werr := <-workerErr; werr != nil {
			fmt.Fprintf(os.Stderr, "worker: %v\n", werr)
			if firstErr == nil {
				firstErr = werr
			}
		}
		cancelAPI()
	case err := <-workerErr:
		if err != nil {
			fmt.Fprintf(os.Stderr, "worker: %v\n", err)
			firstErr = err
		}
		cancelWorker()
		cancelAPI()
		if aerr := <-apiErr; aerr != nil {
			fmt.Fprintf(os.Stderr, "api: %v\n", aerr)
			if firstErr == nil {
				firstErr = aerr
			}
		}
	case <-sigCh:
		cancelWorker()
		if werr := <-workerErr; werr != nil {
			fmt.Fprintf(os.Stderr, "worker: %v\n", werr)
			firstErr = werr
		}
		cancelAPI()
		if aerr := <-apiErr; aerr != nil {
			fmt.Fprintf(os.Stderr, "api: %v\n", aerr)
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
		fmt.Fprintf(os.Stderr, "sandbox: kernel/rootfs paths not configured, falling back to trusted mode\n")
		return
	}

	if _, err := os.Stat(kernelPath); err != nil {
		fmt.Fprintf(os.Stderr, "sandbox: kernel not found (%s), falling back to trusted mode\n", kernelPath)
		return
	}
	if _, err := os.Stat(rootfsPath); err != nil {
		fmt.Fprintf(os.Stderr, "sandbox: rootfs not found (%s), falling back to trusted mode\n", rootfsPath)
		return
	}
	if initrdPath != "" {
		if _, err := os.Stat(initrdPath); err != nil {
			fmt.Fprintf(os.Stderr, "sandbox: initrd not found (%s), proceeding without initrd\n", initrdPath)
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
		fmt.Fprintf(os.Stderr, "sandbox: init failed, falling back to trusted mode: %v\n", err)
		return
	}

	addr, err := srv.Start(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sandbox: start failed, falling back to trusted mode: %v\n", err)
		return
	}

	desktop.SetSandboxAddr(addr)
	fmt.Fprintf(os.Stderr, "sandbox: embedded VZ sandbox listening on %s\n", addr)
}

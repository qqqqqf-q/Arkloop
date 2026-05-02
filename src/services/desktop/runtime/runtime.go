//go:build desktop

package desktopruntime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	api "arkloop/services/api"
	bridge "arkloop/services/bridge"
	desktopsandbox "arkloop/services/sandbox/desktopserver"
	"arkloop/services/shared/desktop"
	sharedlog "arkloop/services/shared/log"
	worker "arkloop/services/worker"
)

type Options struct {
	Component    string
	StartBridge  bool
	StartSandbox bool
}

func Run(ctx context.Context, opts Options) error {
	if err := EnsureToken(); err != nil {
		return fmt.Errorf("ensure desktop token: %w", err)
	}

	component := strings.TrimSpace(opts.Component)
	if component == "" {
		component = "desktop"
	}
	slog.SetDefault(sharedlog.New(sharedlog.Config{Component: component}))

	if ctx == nil {
		ctx = context.Background()
	}

	apiCtx, cancelAPI := context.WithCancel(ctx)
	workerCtx, cancelWorker := context.WithCancel(ctx)
	defer cancelAPI()
	defer cancelWorker()

	if err := worker.InitDesktopInfra(); err != nil {
		return err
	}
	desktop.RestoreExecutionModeFromDisk()
	desktop.SetSidecarProcess(true)
	defer func() {
		if err := desktop.CloseRegisteredSQLite(); err != nil {
			slog.Error("sqlite close", "err", err)
		}
	}()

	apiErr := make(chan error, 1)
	go func() {
		apiErr <- api.StartDesktop(apiCtx)
	}()

	waitCtx, waitCancel := context.WithTimeout(apiCtx, 30*time.Second)
	apiReadyCh := make(chan error, 1)
	go func() {
		apiReadyCh <- desktop.WaitAPIReady(waitCtx)
	}()

	select {
	case err := <-apiReadyCh:
		waitCancel()
		if err != nil {
			return err
		}
	case err := <-apiErr:
		waitCancel()
		return err
	case <-ctx.Done():
		waitCancel()
		return nil
	}

	if opts.StartSandbox {
		StartEmbeddedSandbox(apiCtx)
	}

	workerErr := make(chan error, 1)
	go func() {
		workerErr <- worker.StartDesktop(workerCtx)
	}()

	if opts.StartBridge {
		go func() {
			if err := bridge.StartDesktop(apiCtx); err != nil {
				slog.Error("bridge error", "err", err)
			}
		}()
	}

	var firstErr error
	select {
	case err := <-apiErr:
		if err != nil {
			slog.Error("api error", "err", err)
			firstErr = err
		}
	case err := <-workerErr:
		if err != nil {
			slog.Error("worker error", "err", err)
			firstErr = err
		}
	case <-ctx.Done():
	}

	cancelWorker()
	if firstErr == nil {
		if werr := <-workerErr; werr != nil {
			slog.Error("worker error", "err", werr)
			firstErr = werr
		}
	}
	cancelAPI()
	if firstErr == nil {
		if aerr := <-apiErr; aerr != nil {
			slog.Error("api error", "err", aerr)
			firstErr = aerr
		}
	}
	return firstErr
}

func EnsureToken() error {
	token := strings.TrimSpace(os.Getenv("ARKLOOP_DESKTOP_TOKEN"))
	if token == "" {
		b := make([]byte, 24)
		if _, err := rand.Read(b); err != nil {
			return fmt.Errorf("generate random token: %w", err)
		}
		token = "arkloop-desktop-" + hex.EncodeToString(b)
		if err := os.Setenv("ARKLOOP_DESKTOP_TOKEN", token); err != nil {
			return fmt.Errorf("setenv ARKLOOP_DESKTOP_TOKEN: %w", err)
		}
	}
	if strings.TrimSpace(os.Getenv("ARKLOOP_BRIDGE_AUTH_TOKEN")) == "" {
		if err := os.Setenv("ARKLOOP_BRIDGE_AUTH_TOKEN", token); err != nil {
			return fmt.Errorf("setenv ARKLOOP_BRIDGE_AUTH_TOKEN: %w", err)
		}
	}

	tokenPath, err := TokenPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o700); err != nil {
		return fmt.Errorf("mkdir for token file: %w", err)
	}
	if err := os.WriteFile(tokenPath, []byte(token), 0o600); err != nil {
		return fmt.Errorf("write token file: %w", err)
	}
	return nil
}

func TokenPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user home dir: %w", err)
	}
	return filepath.Join(home, ".arkloop", "desktop.token"), nil
}

func StartEmbeddedSandbox(ctx context.Context) {
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
		socketDir = filepath.Join(home, ".arkloop", "vm", "sessions")
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

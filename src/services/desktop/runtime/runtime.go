//go:build desktop

package desktopruntime

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	api "arkloop/services/api"
	bridge "arkloop/services/bridge"
	desktopsandbox "arkloop/services/sandbox/desktopserver"
	"arkloop/services/shared/desktop"
	sharedlog "arkloop/services/shared/log"
	worker "arkloop/services/worker"
)

const desktopQuietLogsEnv = "ARKLOOP_DESKTOP_QUIET_LOGS"

type Options struct {
	Component    string
	StartBridge  bool
	StartSandbox bool
	Quiet        bool
}

func Run(ctx context.Context, opts Options) error {
	resolveUserShellPATH()

	if err := EnsureToken(); err != nil {
		return fmt.Errorf("ensure desktop token: %w", err)
	}

	component := strings.TrimSpace(opts.Component)
	if component == "" {
		component = "desktop"
	}
	if opts.Quiet {
		restoreQuietLogs := setDesktopQuietLogsEnv()
		defer restoreQuietLogs()
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

func setDesktopQuietLogsEnv() func() {
	previous, hadPrevious := os.LookupEnv(desktopQuietLogsEnv)
	_ = os.Setenv(desktopQuietLogsEnv, "1")
	return func() {
		if hadPrevious {
			_ = os.Setenv(desktopQuietLogsEnv, previous)
			return
		}
		_ = os.Unsetenv(desktopQuietLogsEnv)
	}
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

func resolveUserShellPATH() {
	if runtime.GOOS != "darwin" {
		return
	}
	currentPATH := os.Getenv("PATH")
	shellPATH, err := shellLoginPATH()
	if err != nil || shellPATH == "" {
		return
	}
	merged := mergePATH(shellPATH, currentPATH)
	if merged != "" {
		if err := os.Setenv("PATH", merged); err != nil {
			slog.Warn("desktop: failed to merge user shell PATH", "err", err)
		}
	}
}

func shellLoginPATH() (string, error) {
	const timeout = 5 * time.Second

	source := "/usr/libexec/path_helper"
	if _, err := os.Stat(source); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, source, "-s")
		out, err := cmd.Output()
		if err == nil {
			return parsePathHelperOutput(out), nil
		}
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, "-ilc", "echo __PATH__$PATH__PATH__")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return parseShellPATHOutput(out), nil
}

func parsePathHelperOutput(out []byte) string {
	for _, line := range bytes.Split(out, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if bytes.HasPrefix(line, []byte(`PATH="`)) {
			extracted := bytes.TrimPrefix(line, []byte(`PATH="`))
			extracted = bytes.TrimSuffix(extracted, []byte(`"; export PATH;`))
			extracted = bytes.TrimSuffix(extracted, []byte(`";`))
			return string(extracted)
		}
	}
	return ""
}

func parseShellPATHOutput(out []byte) string {
	const marker = "__PATH__"
	idx := bytes.Index(out, []byte(marker))
	if idx < 0 {
		return ""
	}
	start := idx + len(marker)
	end := bytes.Index(out[start:], []byte(marker))
	if end < 0 {
		return ""
	}
	return string(out[start : start+end])
}

func mergePATH(shellPATH, currentPATH string) string {
	if shellPATH == "" {
		return currentPATH
	}
	shell := filepath.SplitList(shellPATH)
	current := filepath.SplitList(currentPATH)
	seen := make(map[string]struct{}, len(shell)+len(current))
	var merged []string
	for _, entry := range shell {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		entry = filepath.Clean(entry)
		if _, ok := seen[entry]; ok {
			continue
		}
		seen[entry] = struct{}{}
		merged = append(merged, entry)
	}
	for _, entry := range current {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		entry = filepath.Clean(entry)
		if _, ok := seen[entry]; ok {
			continue
		}
		seen[entry] = struct{}{}
		merged = append(merged, entry)
	}
	return strings.Join(merged, string(os.PathListSeparator))
}

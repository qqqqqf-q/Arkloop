package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type ServerState int

const (
	StateStopped ServerState = iota
	StateStarting
	StateRunning
	StateStopping
	StateError
)

const (
	MaxRestarts       = 3
	InitializeTimeout = 30 * time.Second
	ShutdownTimeout   = 5 * time.Second
	RequestTimeout    = 15 * time.Second
)

var contentModifiedBackoff = []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second}

type ServerInstance struct {
	mu           sync.Mutex
	state        ServerState
	readyCh      chan struct{} // closed when state leaves StateStarting
	client       *Client
	transport    *Transport
	cmd          *exec.Cmd
	config       ServerConfig
	restartCount int
	maxRestarts  int
	rootURI      string
	logger       *slog.Logger
	diagReg      *DiagnosticRegistry
	diagUnsub    func()
	onCrash      func()
	generation   atomic.Int64
}

func NewServerInstance(config ServerConfig, rootURI string, logger *slog.Logger, diagReg *DiagnosticRegistry) *ServerInstance {
	return &ServerInstance{
		state:       StateStopped,
		config:      config,
		maxRestarts: MaxRestarts,
		rootURI:     rootURI,
		logger:      logger,
		diagReg:     diagReg,
	}
}

func (si *ServerInstance) ensureStarted(ctx context.Context) error {
	si.mu.Lock()
	switch si.state {
	case StateRunning:
		si.mu.Unlock()
		return nil
	case StateStopping:
		si.mu.Unlock()
		return fmt.Errorf("server is shutting down")
	case StateError:
		if si.restartCount >= si.maxRestarts {
			si.mu.Unlock()
			return fmt.Errorf("max restarts (%d) exceeded", si.maxRestarts)
		}
	case StateStarting:
		// another goroutine is starting; wait for it
		ch := si.readyCh
		si.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return ctx.Err()
		}
		si.mu.Lock()
		if si.state == StateRunning {
			si.mu.Unlock()
			return nil
		}
		si.mu.Unlock()
		return fmt.Errorf("server failed to start")
	}
	si.readyCh = make(chan struct{})
	si.state = StateStarting
	si.mu.Unlock()

	err := si.startProcess(ctx)

	si.mu.Lock()
	defer si.mu.Unlock()

	if err != nil {
		si.state = StateError
		close(si.readyCh)
		return fmt.Errorf("start server: %w", err)
	}
	si.state = StateRunning
	close(si.readyCh)
	return nil
}

func (si *ServerInstance) startProcess(ctx context.Context) error {
	rootPath, err := URIToPath(si.rootURI)
	if err != nil {
		return fmt.Errorf("resolve root path: %w", err)
	}

	cmd := exec.Command(si.config.Command, si.config.Args...)
	cmd.Dir = rootPath
	cmd.Env = si.buildEnv()

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start process: %w", err)
	}

	si.cmd = cmd
	gen := si.generation.Add(1)

	// drain stderr to logger
	go si.drainStderr(stderr)

	transport := NewTransport(stdin, stdout)
	client := NewClient(transport, si.logger)

	// read loop
	go func() {
		transport.handleMessages()
		_ = cmd.Wait()
		si.handleCrash(gen)
	}()

	initCtx, cancel := context.WithTimeout(ctx, InitializeTimeout)
	defer cancel()

	var initOpts any
	if len(si.config.InitOptions) > 0 {
		initOpts = si.config.InitOptions
	}

	if _, err := client.Initialize(initCtx, si.rootURI, initOpts); err != nil {
		// kill only; the goroutine above will call cmd.Wait()
		_ = cmd.Process.Kill()
		return fmt.Errorf("initialize: %w", err)
	}

	si.transport = transport
	si.client = client

	// subscribe to diagnostics notifications
	if si.diagReg != nil {
		si.diagUnsub = transport.Subscribe(MethodPublishDiagnostics, func(_ string, params json.RawMessage) {
			var p PublishDiagnosticsParams
			if json.Unmarshal(params, &p) == nil {
				si.diagReg.HandlePublishDiagnostics(p)
			}
		})
	}

	return nil
}

func (si *ServerInstance) buildEnv() []string {
	env := os.Environ()
	for k, v := range si.config.Env {
		env = append(env, k+"="+v)
	}
	return env
}

func (si *ServerInstance) drainStderr(r io.ReadCloser) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		si.logger.Info("lsp stderr", "line", scanner.Text())
	}
}

func (si *ServerInstance) Execute(ctx context.Context, fn func(*Client) error) error {
	if err := si.ensureStarted(ctx); err != nil {
		return err
	}

	si.mu.Lock()
	client := si.client
	si.mu.Unlock()

	if client == nil {
		return fmt.Errorf("no client available")
	}

	err := fn(client)
	if err == nil {
		return nil
	}

	// retry on ContentModified
	var rpcErr *RPCError
	if errors.As(err, &rpcErr) && rpcErr.Code == ErrContentModified {
		for i := range contentModifiedBackoff {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(contentModifiedBackoff[i]):
			}
			if retryErr := fn(client); retryErr == nil {
				return nil
			}
		}
		return err
	}

	// transport-level error: attempt restart and retry once
	if isTransportError(err) {
		si.mu.Lock()
		si.state = StateError
		si.mu.Unlock()

		if restartErr := si.ensureStarted(ctx); restartErr != nil {
			return fmt.Errorf("restart failed: %w (original: %w)", restartErr, err)
		}

		si.mu.Lock()
		client = si.client
		si.mu.Unlock()

		if retryErr := fn(client); retryErr != nil {
			return retryErr
		}
		return nil
	}

	return err
}

func (si *ServerInstance) Stop(ctx context.Context) error {
	si.mu.Lock()
	if si.state == StateStopped {
		si.mu.Unlock()
		return nil
	}
	si.state = StateStopping
	client := si.client
	transport := si.transport
	cmd := si.cmd
	if si.diagUnsub != nil {
		si.diagUnsub()
		si.diagUnsub = nil
	}
	si.mu.Unlock()

	// shutdown the LSP server
	if client != nil {
		shutdownCtx, cancel := context.WithTimeout(ctx, ShutdownTimeout)
		_ = client.Shutdown(shutdownCtx)
		cancel()
	}

	if transport != nil {
		_ = transport.Close()
	}

	// wait for process exit, escalate if needed
	if cmd != nil && cmd.Process != nil {
		done := make(chan struct{})
		go func() {
			_ = cmd.Wait()
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(2 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	}

	si.mu.Lock()
	si.state = StateStopped
	si.restartCount = 0
	si.client = nil
	si.transport = nil
	si.cmd = nil
	si.mu.Unlock()

	return nil
}

func (si *ServerInstance) handleCrash(gen int64) {
	si.mu.Lock()
	defer si.mu.Unlock()

	if si.state == StateStopping || si.state == StateStopped {
		return
	}

	// stale generation: a new process already started, skip cleanup
	if si.generation.Load() != gen {
		return
	}

	// close old transport
	if si.transport != nil {
		_ = si.transport.Close()
	}

	// kill old process if still alive
	if si.cmd != nil && si.cmd.Process != nil {
		_ = si.cmd.Process.Kill()
	}

	si.restartCount++
	si.state = StateError
	si.client = nil
	si.transport = nil
	si.cmd = nil

	if si.restartCount >= si.maxRestarts {
		si.logger.Error("lsp server max restarts exceeded", "restarts", si.restartCount)
	} else {
		si.logger.Warn("lsp server crashed, will restart on next request", "restarts", si.restartCount)
	}

	if si.onCrash != nil {
		si.onCrash()
	}
}

func isTransportError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "EOF") ||
		strings.Contains(msg, "transport closed") ||
		strings.Contains(msg, "connection reset")
}

package acp

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	localStdoutLimit = 1 << 20
	localStderrLimit = 64 * 1024
	localReadChunk   = 4 * 1024
	localKillGrace   = 2 * time.Second
	localCleanupTTL  = 5 * time.Minute
)

type LocalProcessHost struct{}

func NewLocalProcessHost() ProcessHost {
	return &LocalProcessHost{}
}

type localProcessRegistry struct {
	mu    sync.Mutex
	procs map[string]*localProcess
}

var globalLocalProcesses = &localProcessRegistry{procs: map[string]*localProcess{}}

type localProcess struct {
	id           string
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	stdout       *cursorBuffer
	stderr       *limitedBuffer
	killGrace    time.Duration
	cleanupDelay time.Duration

	mu       sync.Mutex
	exited   bool
	exitCode *int
	exitCh   chan struct{}
}

type cursorBuffer struct {
	buf []byte
}

func newCursorBuffer(limit int) *cursorBuffer {
	if limit <= 0 {
		limit = localStdoutLimit
	}
	return &cursorBuffer{buf: make([]byte, 0, limit)}
}

func (b *cursorBuffer) Append(p []byte) {
	b.buf = append(b.buf, p...)
}

func (b *cursorBuffer) ReadFrom(cursor uint64, limit int) ([]byte, uint64, bool) {
	if limit <= 0 {
		limit = localStdoutLimit
	}
	if cursor >= uint64(len(b.buf)) {
		return nil, uint64(len(b.buf)), false
	}
	start := int(cursor)
	end := len(b.buf)
	truncated := false
	if end-start > limit {
		end = start + limit
		truncated = true
	}
	data := append([]byte(nil), b.buf[start:end]...)
	return data, uint64(end), truncated
}

func (b *cursorBuffer) Bytes() []byte {
	return append([]byte(nil), b.buf...)
}

func (b *cursorBuffer) EndCursor() uint64 {
	return uint64(len(b.buf))
}

type limitedBuffer struct {
	limit int
	buf   []byte
}

func newLimitedBuffer(limit int) *limitedBuffer {
	if limit < 0 {
		limit = 0
	}
	return &limitedBuffer{limit: limit, buf: make([]byte, 0, min(limit, 1024))}
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 || len(b.buf) >= b.limit {
		return len(p), nil
	}
	remaining := b.limit - len(b.buf)
	if len(p) > remaining {
		p = p[:remaining]
	}
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	return string(b.buf)
}

func (h *LocalProcessHost) Start(ctx context.Context, req StartRequest) (*StartResponse, error) {
	p, err := startLocalProcess(req.Command, req.Cwd, req.Env, req.TimeoutMs, req.KillGraceMs, req.CleanupDelayMs)
	if err != nil {
		return nil, err
	}
	globalLocalProcesses.store(p)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return &StartResponse{
		ProcessID:    p.id,
		Status:       "running",
		AgentVersion: "worker-local/1.0",
	}, nil
}

func (h *LocalProcessHost) Write(_ context.Context, req WriteRequest) error {
	p, err := globalLocalProcesses.lookup(req.ProcessID)
	if err != nil {
		return err
	}
	_, err = io.WriteString(p.stdin, req.Data)
	if err != nil {
		return fmt.Errorf("write stdin: %w", err)
	}
	return nil
}

func (h *LocalProcessHost) Read(_ context.Context, req ReadRequest) (*ReadResponse, error) {
	p, err := globalLocalProcesses.lookup(req.ProcessID)
	if err != nil {
		return nil, err
	}
	limit := req.MaxBytes
	if limit <= 0 {
		limit = defaultReadMaxBytes
	}

	p.mu.Lock()
	data, next, truncated := p.stdout.ReadFrom(req.Cursor, limit)
	stderr := p.stderr.String()
	exited := p.exited
	exitCode := p.exitCode
	p.mu.Unlock()

	return &ReadResponse{
		Data:         string(data),
		NextCursor:   next,
		Truncated:    truncated,
		Stderr:       stderr,
		ErrorSummary: extractErrorSummary(stderr, 512),
		Exited:       exited,
		ExitCode:     exitCode,
	}, nil
}

func (h *LocalProcessHost) Stop(_ context.Context, req StopRequest) error {
	p, err := globalLocalProcesses.lookup(req.ProcessID)
	if err != nil {
		return err
	}

	p.mu.Lock()
	alreadyExited := p.exited
	p.mu.Unlock()
	if alreadyExited {
		globalLocalProcesses.remove(p.id)
		return nil
	}

	if req.Force {
		if err := p.cmd.Process.Kill(); err != nil {
			return err
		}
	} else {
		grace := p.killGrace
		if req.GracePeriodMs > 0 {
			grace = time.Duration(req.GracePeriodMs) * time.Millisecond
		}
		if err := p.cmd.Process.Signal(os.Interrupt); err != nil {
			_ = p.cmd.Process.Kill()
		} else {
			go func() {
				select {
				case <-p.exitCh:
				case <-time.After(grace):
					_ = p.cmd.Process.Kill()
				}
			}()
		}
	}

	<-p.exitCh
	globalLocalProcesses.remove(p.id)
	return nil
}

func (h *LocalProcessHost) Wait(ctx context.Context, req WaitRequest) (*WaitResponse, error) {
	p, err := globalLocalProcesses.lookup(req.ProcessID)
	if err != nil {
		return nil, err
	}

	if req.TimeoutMs > 0 {
		select {
		case <-p.exitCh:
		case <-time.After(time.Duration(req.TimeoutMs) * time.Millisecond):
			return &WaitResponse{Exited: false}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	} else {
		select {
		case <-p.exitCh:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	return &WaitResponse{
		Exited:   true,
		ExitCode: p.exitCode,
		Stdout:   string(p.stdout.Bytes()),
		Stderr:   p.stderr.String(),
	}, nil
}

func (h *LocalProcessHost) Status(_ context.Context, req StatusRequest) (*StatusResponse, error) {
	p, err := globalLocalProcesses.lookup(req.ProcessID)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	return &StatusResponse{
		SessionID:    req.SessionID,
		ProcessID:    req.ProcessID,
		Running:      !p.exited,
		StdoutCursor: p.stdout.EndCursor(),
		Exited:       p.exited,
		ExitCode:     p.exitCode,
	}, nil
}

func (r *localProcessRegistry) store(p *localProcess) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.procs[p.id] = p
}

func (r *localProcessRegistry) lookup(id string) (*localProcess, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.procs[id]
	if !ok {
		return nil, fmt.Errorf("acp process %s not found", id)
	}
	return p, nil
}

func (r *localProcessRegistry) remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.procs, id)
}

func startLocalProcess(command []string, cwd string, env map[string]string, timeoutMs int, killGraceMs int, cleanupDelayMs int) (*localProcess, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("command must not be empty")
	}

	cmd := exec.Command(command[0], command[1:]...)
	if trimmed := strings.TrimSpace(cwd); trimmed != "" {
		cmd.Dir = trimmed
	}
	if len(env) > 0 {
		merged := os.Environ()
		for k, v := range env {
			merged = append(merged, k+"="+v)
		}
		cmd.Env = merged
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start process: %w", err)
	}

	p := &localProcess{
		id:           uuid.NewString(),
		cmd:          cmd,
		stdin:        stdin,
		stdout:       newCursorBuffer(localStdoutLimit),
		stderr:       newLimitedBuffer(localStderrLimit),
		killGrace:    durationOrDefault(killGraceMs, localKillGrace),
		cleanupDelay: durationOrDefault(cleanupDelayMs, localCleanupTTL),
		exitCh:       make(chan struct{}),
	}

	var pumpWG sync.WaitGroup
	pumpWG.Add(2)
	go p.pump(stdout, true, &pumpWG)
	go p.pump(stderr, false, &pumpWG)
	go p.waitLoop(&pumpWG)

	if timeoutMs > 0 {
		go p.enforceTimeout(time.Duration(timeoutMs) * time.Millisecond)
	}
	go func() {
		<-p.exitCh
		time.Sleep(p.cleanupDelay)
		globalLocalProcesses.remove(p.id)
	}()

	return p, nil
}

func (p *localProcess) pump(r io.Reader, isStdout bool, wg *sync.WaitGroup) {
	defer wg.Done()
	buf := make([]byte, localReadChunk)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			p.mu.Lock()
			if isStdout {
				p.stdout.Append(buf[:n])
			} else {
				_, _ = p.stderr.Write(buf[:n])
			}
			p.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (p *localProcess) waitLoop(pumpWG *sync.WaitGroup) {
	pumpWG.Wait()
	err := p.cmd.Wait()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			code = -1
		}
	}

	p.mu.Lock()
	p.exitCode = &code
	p.exited = true
	p.mu.Unlock()
	close(p.exitCh)
}

func (p *localProcess) enforceTimeout(timeout time.Duration) {
	select {
	case <-p.exitCh:
	case <-time.After(timeout):
		_ = p.cmd.Process.Kill()
	}
}

func durationOrDefault(valueMs int, fallback time.Duration) time.Duration {
	if valueMs <= 0 {
		return fallback
	}
	return time.Duration(valueMs) * time.Millisecond
}

func extractErrorSummary(stderr string, maxLen int) string {
	if stderr == "" {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		lower := strings.ToLower(line)
		if strings.Contains(lower, "error") || strings.Contains(lower, "panic") ||
			strings.Contains(lower, "fatal") || strings.Contains(lower, "fail") {
			return truncateString(line, maxLen)
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return truncateString(line, maxLen)
		}
	}
	return ""
}

func truncateString(value string, maxLen int) string {
	if maxLen <= 0 || len(value) <= maxLen {
		return value
	}
	return value[:maxLen]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

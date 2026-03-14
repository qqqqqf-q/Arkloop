package main

import (
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	shellapi "arkloop/services/sandbox/internal/shell"

	"github.com/google/uuid"
)

// ---------- request / response ----------

type ACPStartRequest struct {
	Command        []string          `json:"command"`
	Cwd            string            `json:"cwd,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	TimeoutMs      int               `json:"timeout_ms,omitempty"`
	KillGraceMs    int               `json:"kill_grace_ms,omitempty"`
	CleanupDelayMs int               `json:"cleanup_delay_ms,omitempty"`
}

type ACPStartResponse struct {
	ProcessID    string `json:"process_id"`
	Status       string `json:"status"`
	AgentVersion string `json:"agent_version,omitempty"`
}

type ACPWriteRequest struct {
	ProcessID string `json:"process_id"`
	Data      string `json:"data"`
}

type ACPWriteResponse struct {
	BytesWritten int `json:"bytes_written"`
}

type ACPReadRequest struct {
	ProcessID string `json:"process_id"`
	Cursor    uint64 `json:"cursor"`
	MaxBytes  int    `json:"max_bytes,omitempty"`
}

type ACPReadResponse struct {
	Data         string `json:"data"`
	NextCursor   uint64 `json:"next_cursor"`
	Truncated    bool   `json:"truncated"`
	Stderr       string `json:"stderr,omitempty"`
	ErrorSummary string `json:"error_summary,omitempty"`
	Exited       bool   `json:"exited"`
	ExitCode     *int   `json:"exit_code,omitempty"`
}

type ACPStopRequest struct {
	ProcessID     string `json:"process_id"`
	Force         bool   `json:"force,omitempty"`
	GracePeriodMs int    `json:"grace_period_ms,omitempty"`
}

type ACPStopResponse struct {
	Status string `json:"status"`
}

type ACPWaitRequest struct {
	ProcessID string `json:"process_id"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
}

type ACPWaitResponse struct {
	Exited   bool   `json:"exited"`
	ExitCode *int   `json:"exit_code,omitempty"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
}

type ACPStatusRequest struct {
	ProcessID string `json:"process_id"`
}

type ACPStatusResponse struct {
	Running      bool   `json:"running"`
	StdoutCursor uint64 `json:"stdout_cursor"`
	Exited       bool   `json:"exited"`
	ExitCode     *int   `json:"exit_code,omitempty"`
}

// ---------- internal ----------

const (
	acpStdoutBufSize = shellapi.RingBufferBytes // 1 MB
	acpStderrBufSize = 64 * 1024                // 64 KB
	acpReadBufChunk  = 4 * 1024                 // io copy chunk
	acpCleanupDelay  = 5 * time.Minute
	acpKillGrace     = 2 * time.Second
)

type acpProcess struct {
	id           string
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	stdout       *shellapi.RingBuffer
	stderr       *limitedBuffer
	killGrace    time.Duration
	cleanupDelay time.Duration

	mu       sync.Mutex
	exitCode *int
	exited   bool
	exitCh   chan struct{}
}

type ACPManager struct {
	mu        sync.Mutex
	processes map[string]*acpProcess
}

func NewACPManager() *ACPManager {
	return &ACPManager{processes: make(map[string]*acpProcess)}
}

// ---------- Start ----------

func (m *ACPManager) Start(req ACPStartRequest) (*ACPStartResponse, error) {
	if len(req.Command) == 0 {
		return nil, fmt.Errorf("command must not be empty")
	}

	cmd := exec.Command(req.Command[0], req.Command[1:]...)
	prepareWorkloadCmd(cmd, req.Cwd, req.Env)

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start process: %w", err)
	}

	kg := acpKillGrace
	if req.KillGraceMs > 0 {
		kg = time.Duration(req.KillGraceMs) * time.Millisecond
	}
	cd := acpCleanupDelay
	if req.CleanupDelayMs > 0 {
		cd = time.Duration(req.CleanupDelayMs) * time.Millisecond
	}

	p := &acpProcess{
		id:           uuid.NewString(),
		cmd:          cmd,
		stdin:        stdinPipe,
		stdout:       shellapi.NewRingBuffer(acpStdoutBufSize),
		stderr:       newLimitedBuffer(acpStderrBufSize),
		killGrace:    kg,
		cleanupDelay: cd,
		exitCh:       make(chan struct{}),
	}

	go p.pumpOutput(stdoutPipe, true)
	go p.pumpOutput(stderrPipe, false)
	go p.waitLoop()

	if req.TimeoutMs > 0 {
		go p.enforceTimeout(time.Duration(req.TimeoutMs) * time.Millisecond)
	}

	m.mu.Lock()
	m.processes[p.id] = p
	m.mu.Unlock()

	go m.scheduleCleanup(p)

	return &ACPStartResponse{ProcessID: p.id, Status: "running", AgentVersion: "guest-agent/1.0"}, nil
}

// ---------- Write ----------

func (m *ACPManager) Write(req ACPWriteRequest) (*ACPWriteResponse, error) {
	p, err := m.lookup(req.ProcessID)
	if err != nil {
		return nil, err
	}
	n, err := io.WriteString(p.stdin, req.Data)
	if err != nil {
		return nil, fmt.Errorf("write stdin: %w", err)
	}
	return &ACPWriteResponse{BytesWritten: n}, nil
}

// ---------- Read ----------

func (m *ACPManager) Read(req ACPReadRequest) (*ACPReadResponse, error) {
	p, err := m.lookup(req.ProcessID)
	if err != nil {
		return nil, err
	}

	limit := req.MaxBytes
	if limit <= 0 {
		limit = shellapi.ReadChunkBytes
	}

	p.mu.Lock()
	data, next, truncated, _ := p.stdout.ReadFrom(req.Cursor, limit)
	stderrSnap := p.stderr.String()
	exited := p.exited
	var code *int
	if exited {
		code = p.exitCode
	}
	p.mu.Unlock()

	errSummary := ""
	if stderrSnap != "" {
		errSummary = extractErrorSummary(stderrSnap, 512)
	}

	return &ACPReadResponse{
		Data:         string(data),
		NextCursor:   next,
		Truncated:    truncated,
		Stderr:       stderrSnap,
		ErrorSummary: errSummary,
		Exited:       exited,
		ExitCode:     code,
	}, nil
}

// ---------- Stop ----------

func (m *ACPManager) Stop(req ACPStopRequest) (*ACPStopResponse, error) {
	p, err := m.lookup(req.ProcessID)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	already := p.exited
	p.mu.Unlock()
	if already {
		m.remove(p.id)
		return &ACPStopResponse{Status: "already_exited"}, nil
	}

	if req.Force {
		_ = p.cmd.Process.Kill()
	} else {
		grace := p.killGrace
		if req.GracePeriodMs > 0 {
			grace = time.Duration(req.GracePeriodMs) * time.Millisecond
		}
		_ = p.cmd.Process.Signal(syscall.SIGINT)
		go func() {
			select {
			case <-p.exitCh:
			case <-time.After(grace):
				_ = p.cmd.Process.Kill()
			}
		}()
	}

	<-p.exitCh
	m.remove(p.id)
	return &ACPStopResponse{Status: "stopped"}, nil
}

// ---------- Wait ----------

func (m *ACPManager) Wait(req ACPWaitRequest) (*ACPWaitResponse, error) {
	p, err := m.lookup(req.ProcessID)
	if err != nil {
		return nil, err
	}

	if req.TimeoutMs > 0 {
		select {
		case <-p.exitCh:
		case <-time.After(time.Duration(req.TimeoutMs) * time.Millisecond):
			return &ACPWaitResponse{Exited: false}, nil
		}
	} else {
		<-p.exitCh
	}

	p.mu.Lock()
	resp := &ACPWaitResponse{
		Exited:   true,
		ExitCode: p.exitCode,
		Stdout:   string(p.stdout.Bytes()),
		Stderr:   p.stderr.String(),
	}
	p.mu.Unlock()
	return resp, nil
}

// ---------- Status ----------

func (m *ACPManager) Status(req ACPStatusRequest) (*ACPStatusResponse, error) {
	p, err := m.lookup(req.ProcessID)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	running := !p.exited
	exited := p.exited
	var code *int
	if exited {
		code = p.exitCode
	}
	cursor := p.stdout.EndCursor()
	p.mu.Unlock()

	return &ACPStatusResponse{
		Running:      running,
		StdoutCursor: cursor,
		Exited:       exited,
		ExitCode:     code,
	}, nil
}

// ---------- helpers ----------

func (m *ACPManager) lookup(id string) (*acpProcess, error) {
	m.mu.Lock()
	p, ok := m.processes[id]
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("acp process %s not found", id)
	}
	return p, nil
}

func (m *ACPManager) remove(id string) {
	m.mu.Lock()
	delete(m.processes, id)
	m.mu.Unlock()
}

// 进程退出后延迟清理，避免客户端来不及读取最终输出
func (m *ACPManager) scheduleCleanup(p *acpProcess) {
	<-p.exitCh
	time.Sleep(p.cleanupDelay)
	m.remove(p.id)
}

// ---------- acpProcess goroutines ----------

func (p *acpProcess) pumpOutput(r io.Reader, isStdout bool) {
	buf := make([]byte, acpReadBufChunk)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			p.mu.Lock()
			if isStdout {
				p.stdout.Append(buf[:n])
			} else {
				p.stderr.Write(buf[:n])
			}
			p.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (p *acpProcess) waitLoop() {
	err := p.cmd.Wait()
	code := 0
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
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

func (p *acpProcess) enforceTimeout(d time.Duration) {
	select {
	case <-p.exitCh:
	case <-time.After(d):
		_ = p.cmd.Process.Kill()
	}
}

// extractErrorSummary extracts the last meaningful error line from stderr.
func extractErrorSummary(stderr string, maxLen int) string {
	if stderr == "" {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	// scan from the end for lines containing error indicators
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		lower := strings.ToLower(line)
		if strings.Contains(lower, "error") || strings.Contains(lower, "panic") ||
			strings.Contains(lower, "fatal") || strings.Contains(lower, "fail") {
			if len(line) > maxLen {
				return line[:maxLen]
			}
			return line
		}
	}
	// no error keyword found, return last non-empty line
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			if len(line) > maxLen {
				return line[:maxLen]
			}
			return line
		}
	}
	return ""
}

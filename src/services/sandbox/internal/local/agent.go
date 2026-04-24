package local

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	shellapi "arkloop/services/sandbox/internal/shell"
)

// --- ACP agent protocol types (JSON-compatible with internal/acp/protocol.go) ---

type agentRequest struct {
	Action   string           `json:"action"`
	ACPStart *acpStartPayload `json:"acp_start,omitempty"`
	ACPWrite *acpWritePayload `json:"acp_write,omitempty"`
	ACPRead  *acpReadPayload  `json:"acp_read,omitempty"`
	ACPStop    *acpStopPayload    `json:"acp_stop,omitempty"`
	ACPWait    *acpWaitPayload    `json:"acp_wait,omitempty"`
	ACPStatus  *acpStatusPayload  `json:"acp_status,omitempty"`
}

type agentResponse struct {
	Action   string          `json:"action"`
	ACPStart *acpStartResult `json:"acp_start,omitempty"`
	ACPWrite *acpWriteResult `json:"acp_write,omitempty"`
	ACPRead  *acpReadResult  `json:"acp_read,omitempty"`
	ACPStop    *acpStopResult    `json:"acp_stop,omitempty"`
	ACPWait    *acpWaitResult    `json:"acp_wait,omitempty"`
	ACPStatus  *acpStatusResult  `json:"acp_status,omitempty"`
	Error      string            `json:"error,omitempty"`
}

type acpStartPayload struct {
	Command        []string          `json:"command"`
	Cwd            string            `json:"cwd,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	TimeoutMs      int               `json:"timeout_ms,omitempty"`
	KillGraceMs    int               `json:"kill_grace_ms,omitempty"`
	CleanupDelayMs int               `json:"cleanup_delay_ms,omitempty"`
}

type acpStartResult struct {
	ProcessID    string `json:"process_id"`
	Status       string `json:"status"`
	AgentVersion string `json:"agent_version,omitempty"`
}

type acpWritePayload struct {
	ProcessID string `json:"process_id"`
	Data      string `json:"data"`
}

type acpWriteResult struct {
	BytesWritten int `json:"bytes_written"`
}

type acpReadPayload struct {
	ProcessID string `json:"process_id"`
	Cursor    uint64 `json:"cursor"`
	MaxBytes  int    `json:"max_bytes,omitempty"`
}

type acpReadResult struct {
	Data         string `json:"data"`
	NextCursor   uint64 `json:"next_cursor"`
	Truncated    bool   `json:"truncated"`
	Stderr       string `json:"stderr,omitempty"`
	ErrorSummary string `json:"error_summary,omitempty"`
	Exited       bool   `json:"exited"`
	ExitCode     *int   `json:"exit_code,omitempty"`
}

type acpStopPayload struct {
	ProcessID     string `json:"process_id"`
	Force         bool   `json:"force,omitempty"`
	GracePeriodMs int    `json:"grace_period_ms,omitempty"`
}

type acpStopResult struct {
	Status string `json:"status"`
}

type acpWaitPayload struct {
	ProcessID string `json:"process_id"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
}

type acpWaitResult struct {
	Exited   bool   `json:"exited"`
	ExitCode *int   `json:"exit_code,omitempty"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
}

type acpStatusPayload struct {
	ProcessID string `json:"process_id"`
}

type acpStatusResult struct {
	Running      bool   `json:"running"`
	StdoutCursor uint64 `json:"stdout_cursor"`
	Exited       bool   `json:"exited"`
	ExitCode     *int   `json:"exit_code,omitempty"`
}

// --- Agent ---

// Agent is an embedded TCP server that handles ACP protocol requests
// for managing local processes. It listens on 127.0.0.1:<random_port>.
type Agent struct {
	id       string
	listener net.Listener
	mu       sync.Mutex
	procs    map[string]*process
	stop     chan struct{}
	wg       sync.WaitGroup
}

// NewAgent starts a TCP listener on a random port and begins accepting connections.
func NewAgent(id string) (*Agent, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}
	a := &Agent{
		id:       id,
		listener: ln,
		procs:    make(map[string]*process),
		stop:     make(chan struct{}),
	}
	a.wg.Add(1)
	go a.serve()
	return a, nil
}

// Addr returns the listener address (e.g. "127.0.0.1:12345").
func (a *Agent) Addr() string {
	return a.listener.Addr().String()
}

// Close stops the listener and kills all managed processes.
func (a *Agent) Close() error {
	close(a.stop)
	err := a.listener.Close()
	a.wg.Wait()

	a.mu.Lock()
	for _, p := range a.procs {
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
	}
	a.procs = make(map[string]*process)
	a.mu.Unlock()

	return err
}

func (a *Agent) serve() {
	defer a.wg.Done()
	for {
		conn, err := a.listener.Accept()
		if err != nil {
			select {
			case <-a.stop:
				return
			default:
				continue
			}
		}
		go a.handleConn(conn)
	}
}

func (a *Agent) handleConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()

	var req agentRequest
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		writeResponse(conn, agentResponse{Error: fmt.Sprintf("decode request: %v", err)})
		return
	}

	resp := a.dispatch(req)
	writeResponse(conn, resp)
}

func writeResponse(conn net.Conn, resp agentResponse) {
	_ = json.NewEncoder(conn).Encode(resp)
}

func (a *Agent) dispatch(req agentRequest) agentResponse {
	switch req.Action {
	case "acp_start":
		return a.handleStart(req)
	case "acp_write":
		return a.handleWrite(req)
	case "acp_read":
		return a.handleRead(req)
	case "acp_stop":
		return a.handleStop(req)
	case "acp_wait":
		return a.handleWait(req)
	case "acp_status":
		return a.handleStatus(req)
	default:
		return agentResponse{Action: req.Action, Error: fmt.Sprintf("unknown action: %s", req.Action)}
	}
}

func (a *Agent) handleStart(req agentRequest) agentResponse {
	if req.ACPStart == nil {
		return agentResponse{Action: req.Action, Error: "acp_start payload is required"}
	}
	p, err := startProcess(req.ACPStart.Command, req.ACPStart.Cwd, req.ACPStart.Env, req.ACPStart.TimeoutMs, req.ACPStart.KillGraceMs, req.ACPStart.CleanupDelayMs)
	if err != nil {
		return agentResponse{Action: req.Action, Error: err.Error()}
	}

	a.mu.Lock()
	a.procs[p.id] = p
	a.mu.Unlock()

	go a.scheduleCleanup(p)

	return agentResponse{
		Action:   req.Action,
		ACPStart: &acpStartResult{ProcessID: p.id, Status: "running", AgentVersion: "local-sandbox/1.0"},
	}
}

func (a *Agent) handleWrite(req agentRequest) agentResponse {
	if req.ACPWrite == nil {
		return agentResponse{Action: req.Action, Error: "acp_write payload is required"}
	}
	p, err := a.lookup(req.ACPWrite.ProcessID)
	if err != nil {
		return agentResponse{Action: req.Action, Error: err.Error()}
	}
	n, err := io.WriteString(p.stdin, req.ACPWrite.Data)
	if err != nil {
		return agentResponse{Action: req.Action, Error: fmt.Sprintf("write stdin: %v", err)}
	}
	return agentResponse{
		Action:   req.Action,
		ACPWrite: &acpWriteResult{BytesWritten: n},
	}
}

func (a *Agent) handleRead(req agentRequest) agentResponse {
	if req.ACPRead == nil {
		return agentResponse{Action: req.Action, Error: "acp_read payload is required"}
	}
	p, err := a.lookup(req.ACPRead.ProcessID)
	if err != nil {
		return agentResponse{Action: req.Action, Error: err.Error()}
	}

	limit := req.ACPRead.MaxBytes
	if limit <= 0 {
		limit = shellapi.ReadChunkBytes
	}

	p.mu.Lock()
	data, next, truncated, _ := p.stdout.ReadFrom(req.ACPRead.Cursor, limit)
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

	return agentResponse{
		Action: req.Action,
		ACPRead: &acpReadResult{
			Data:         string(data),
			NextCursor:   next,
			Truncated:    truncated,
			Stderr:       stderrSnap,
			ErrorSummary: errSummary,
			Exited:       exited,
			ExitCode:     code,
		},
	}
}

func (a *Agent) handleStop(req agentRequest) agentResponse {
	if req.ACPStop == nil {
		return agentResponse{Action: req.Action, Error: "acp_stop payload is required"}
	}
	p, err := a.lookup(req.ACPStop.ProcessID)
	if err != nil {
		return agentResponse{Action: req.Action, Error: err.Error()}
	}

	p.mu.Lock()
	already := p.exited
	p.mu.Unlock()

	if already {
		a.remove(p.id)
		return agentResponse{
			Action:  req.Action,
			ACPStop: &acpStopResult{Status: "already_exited"},
		}
	}

	if req.ACPStop.Force {
		_ = p.cmd.Process.Kill()
	} else {
		grace := p.killGrace
		if req.ACPStop.GracePeriodMs > 0 {
			grace = time.Duration(req.ACPStop.GracePeriodMs) * time.Millisecond
		}
		_ = p.cmd.Process.Signal(os.Interrupt)
		go func() {
			select {
			case <-p.exitCh:
			case <-time.After(grace):
				_ = p.cmd.Process.Kill()
			}
		}()
	}

	<-p.exitCh
	a.remove(p.id)

	return agentResponse{
		Action:  req.Action,
		ACPStop: &acpStopResult{Status: "stopped"},
	}
}

func (a *Agent) handleWait(req agentRequest) agentResponse {
	if req.ACPWait == nil {
		return agentResponse{Action: req.Action, Error: "acp_wait payload is required"}
	}
	p, err := a.lookup(req.ACPWait.ProcessID)
	if err != nil {
		return agentResponse{Action: req.Action, Error: err.Error()}
	}

	if req.ACPWait.TimeoutMs > 0 {
		select {
		case <-p.exitCh:
		case <-time.After(time.Duration(req.ACPWait.TimeoutMs) * time.Millisecond):
			return agentResponse{
				Action:  req.Action,
				ACPWait: &acpWaitResult{Exited: false},
			}
		}
	} else {
		<-p.exitCh
	}

	p.mu.Lock()
	resp := agentResponse{
		Action: req.Action,
		ACPWait: &acpWaitResult{
			Exited:   true,
			ExitCode: p.exitCode,
			Stdout:   string(p.stdout.Bytes()),
			Stderr:   p.stderr.String(),
		},
	}
	p.mu.Unlock()

	return resp
}

func (a *Agent) handleStatus(req agentRequest) agentResponse {
	if req.ACPStatus == nil {
		return agentResponse{Action: req.Action, Error: "acp_status payload is required"}
	}
	p, err := a.lookup(req.ACPStatus.ProcessID)
	if err != nil {
		return agentResponse{Action: req.Action, Error: err.Error()}
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

	return agentResponse{
		Action: req.Action,
		ACPStatus: &acpStatusResult{
			Running:      running,
			StdoutCursor: cursor,
			Exited:       exited,
			ExitCode:     code,
		},
	}
}

func (a *Agent) lookup(id string) (*process, error) {
	a.mu.Lock()
	p, ok := a.procs[id]
	a.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("acp process %s not found", id)
	}
	return p, nil
}

func (a *Agent) remove(id string) {
	a.mu.Lock()
	delete(a.procs, id)
	a.mu.Unlock()
}

func (a *Agent) scheduleCleanup(p *process) {
	<-p.exitCh
	time.Sleep(p.cleanupDelay)
	a.remove(p.id)
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

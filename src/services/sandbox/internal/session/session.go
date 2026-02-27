package session

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// ExecJob 是发送给 Guest Agent 的执行任务。
type ExecJob struct {
	Language  string `json:"language"`   // "python" | "shell"
	Code      string `json:"code"`
	TimeoutMs int    `json:"timeout_ms"`
}

// ExecResult 是 Guest Agent 返回的执行结果。
type ExecResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

// Session 对应一个 Firecracker microVM 的执行上下文。
type Session struct {
	ID        string
	Tier      string
	VsockPath string    // Firecracker vsock UDS 路径
	AgentPort uint32    // Guest Agent 的 vsock 端口号
	CreatedAt time.Time
	SocketDir string    // microVM socket 目录的实际路径（用于清理）

	// 超时管理
	LastActiveAt time.Time
	IdleTimeout  time.Duration
	MaxLifetime  time.Duration

	timerMu       sync.Mutex
	idleTimer     *time.Timer
	lifetimeTimer *time.Timer
	onExpired     func(string) // callback: session ID -> 由 Manager 设置
}

// StartTimers 启动空闲超时和最大存活 timer。
// onExpired 在 timer 触发时被调用（在独立 goroutine 中）。
func (s *Session) StartTimers(onExpired func(string)) {
	s.timerMu.Lock()
	defer s.timerMu.Unlock()

	s.onExpired = onExpired
	s.LastActiveAt = time.Now()

	if s.MaxLifetime > 0 {
		s.lifetimeTimer = time.AfterFunc(s.MaxLifetime, func() {
			s.onExpired(s.ID)
		})
	}
	if s.IdleTimeout > 0 {
		s.idleTimer = time.AfterFunc(s.IdleTimeout, func() {
			s.onExpired(s.ID)
		})
	}
}

// TouchActivity 更新最近活跃时间并重置空闲 timer。
func (s *Session) TouchActivity() {
	s.timerMu.Lock()
	defer s.timerMu.Unlock()

	s.LastActiveAt = time.Now()
	if s.idleTimer != nil && s.IdleTimeout > 0 {
		s.idleTimer.Stop()
		s.idleTimer = time.AfterFunc(s.IdleTimeout, func() {
			s.onExpired(s.ID)
		})
	}
}

// StopTimers 停止所有 timer。Delete session 时调用。
func (s *Session) StopTimers() {
	s.timerMu.Lock()
	defer s.timerMu.Unlock()

	if s.idleTimer != nil {
		s.idleTimer.Stop()
		s.idleTimer = nil
	}
	if s.lifetimeTimer != nil {
		s.lifetimeTimer.Stop()
		s.lifetimeTimer = nil
	}
}

// Exec 在 Session 关联的 microVM Guest Agent 中执行代码。
func (s *Session) Exec(ctx context.Context, job ExecJob) (*ExecResult, error) {
	s.TouchActivity()

	timeout := time.Duration(job.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout+5*time.Second) // +5s for vsock overhead
	defer cancel()

	conn, err := s.connectToAgent(execCtx)
	if err != nil {
		return nil, fmt.Errorf("connect to agent: %w", err)
	}
	defer conn.Close()

	deadline := time.Now().Add(timeout + 5*time.Second)
	_ = conn.SetDeadline(deadline)

	if err := json.NewEncoder(conn).Encode(job); err != nil {
		return nil, fmt.Errorf("send job: %w", err)
	}

	var result ExecResult
	if err := json.NewDecoder(conn).Decode(&result); err != nil {
		return nil, fmt.Errorf("read result: %w", err)
	}
	return &result, nil
}

// connectToAgent 通过 Firecracker vsock 握手协议建立 HOST→GUEST 连接。
//
// Firecracker vsock 握手：
//   HOST: CONNECT {port}\n
//   GUEST: OK {ephemeral_port}\n
func (s *Session) connectToAgent(ctx context.Context) (net.Conn, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", s.VsockPath)
	if err != nil {
		return nil, err
	}

	if _, err := fmt.Fprintf(conn, "CONNECT %d\n", s.AgentPort); err != nil {
		conn.Close()
		return nil, fmt.Errorf("vsock handshake write: %w", err)
	}

	reader := bufio.NewReaderSize(conn, 64)
	line, err := reader.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("vsock handshake read: %w", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(line), "OK") {
		conn.Close()
		return nil, fmt.Errorf("vsock handshake failed: %q", line)
	}

	// reader 可能已缓冲部分数据；包装成可读写 conn
	return &vsockConn{Conn: conn, reader: reader}, nil
}

// vsockConn 将 bufio.Reader（握手后可能有缓冲）和原始 Conn 合并为 net.Conn。
type vsockConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *vsockConn) Read(b []byte) (int, error) {
	return c.reader.Read(b)
}

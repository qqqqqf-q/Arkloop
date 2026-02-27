package session

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
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
}

// Exec 在 Session 关联的 microVM Guest Agent 中执行代码。
func (s *Session) Exec(ctx context.Context, job ExecJob) (*ExecResult, error) {
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

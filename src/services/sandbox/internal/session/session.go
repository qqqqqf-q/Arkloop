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

// ExecJob は Guest Agent へ送信する実行ジョブ。
type ExecJob struct {
	Language  string `json:"language"`   // "python" | "shell"
	Code      string `json:"code"`
	TimeoutMs int    `json:"timeout_ms"`
}

// ExecResult は Guest Agent から受信する実行結果。
type ExecResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

// Session は一つの Firecracker microVM に対応する実行コンテキスト。
type Session struct {
	ID        string
	Tier      string
	VsockPath string    // Firecracker vsock UDS パス
	AgentPort uint32    // Guest Agent の vsock ポート番号
	CreatedAt time.Time
}

// Exec は Session に紐づく microVM の Guest Agent でコードを実行する。
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

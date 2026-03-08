package session

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// ExecJob 是发送给 Guest Agent 的执行任务。
type ExecJob struct {
	Language  string `json:"language"` // "python" | "shell"
	Code      string `json:"code"`
	TimeoutMs int    `json:"timeout_ms"`
}

// ExecResult 是 Guest Agent 返回的执行结果。
type ExecResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

// ArtifactEntry 描述 microVM 输出目录中的一个文件。
type ArtifactEntry struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	MimeType string `json:"mime_type"`
	Data     string `json:"data"` // base64
}

// FetchArtifactsResult 是 fetch_artifacts 请求的响应。
type FetchArtifactsResult struct {
	Artifacts []ArtifactEntry `json:"artifacts"`
	Truncated bool            `json:"truncated"`
}

// agentRequest 是 v2 协议的请求格式。
type agentRequest struct {
	Action      string               `json:"action"`
	Network     *GuestNetworkRequest `json:"network,omitempty"`
	Environment *EnvironmentRequest  `json:"environment,omitempty"`
}

// agentResponse 是 v2 协议的响应格式。
type agentResponse struct {
	Action      string                `json:"action"`
	Artifacts   *FetchArtifactsResult `json:"artifacts,omitempty"`
	Environment *EnvironmentResponse  `json:"environment,omitempty"`
	Error       string                `json:"error,omitempty"`
}

type EnvironmentRequest struct {
	Scope   string `json:"scope"`
	Archive string `json:"archive,omitempty"`
}

type EnvironmentResponse struct {
	Archive string `json:"archive,omitempty"`
}

type GuestNetworkRequest struct {
	Interface   string   `json:"interface"`
	GuestCIDR   string   `json:"guest_cidr"`
	Gateway     string   `json:"gateway"`
	Nameservers []string `json:"nameservers,omitempty"`
}

// Dialer 抽象与 Guest Agent 的连接建立。
// Firecracker 使用 vsock，Docker 使用 TCP。
type Dialer func(ctx context.Context) (net.Conn, error)

// Session 对应一个隔离执行环境（Firecracker microVM 或 Docker 容器）的执行上下文。
type Session struct {
	ID        string
	Tier      string
	OrgID     string // 所属组织，用于跨租户隔离校验
	CreatedAt time.Time
	SocketDir string // 关联资源目录的实际路径（用于清理）

	// 与 Guest Agent 建立连接的方式，由具体 Pool 实现注入
	Dial Dialer

	// 超时管理
	LastActiveAt time.Time
	IdleTimeout  time.Duration
	MaxLifetime  time.Duration

	timerMu       sync.Mutex
	idleTimer     *time.Timer
	lifetimeTimer *time.Timer
	onExpired     func(string, ExpiryReason) // callback: session ID -> 由 Manager 设置
}

type ExpiryReason string

const (
	ExpiryReasonIdleTimeout ExpiryReason = "idle_timeout"
	ExpiryReasonMaxLifetime ExpiryReason = "max_lifetime"
)

// StartTimers 启动空闲超时和最大存活 timer。
// onExpired 在 timer 触发时被调用（在独立 goroutine 中）。
func (s *Session) StartTimers(onExpired func(string, ExpiryReason)) {
	s.timerMu.Lock()
	defer s.timerMu.Unlock()

	s.onExpired = onExpired
	s.LastActiveAt = time.Now()

	if s.MaxLifetime > 0 {
		s.lifetimeTimer = time.AfterFunc(s.MaxLifetime, func() {
			s.onExpired(s.ID, ExpiryReasonMaxLifetime)
		})
	}
	if s.IdleTimeout > 0 {
		s.idleTimer = time.AfterFunc(s.IdleTimeout, func() {
			s.onExpired(s.ID, ExpiryReasonIdleTimeout)
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
			s.onExpired(s.ID, ExpiryReasonIdleTimeout)
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

// Exec 在 Session 关联的隔离环境 Guest Agent 中执行代码。
func (s *Session) Exec(ctx context.Context, job ExecJob) (*ExecResult, error) {
	s.TouchActivity()

	timeout := time.Duration(job.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout+5*time.Second)
	defer cancel()

	conn, err := s.Dial(execCtx)
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

// FetchArtifacts 请求 guest agent 返回输出目录中的文件。
func (s *Session) FetchArtifacts(ctx context.Context) (*FetchArtifactsResult, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	conn, err := s.Dial(fetchCtx)
	if err != nil {
		return nil, fmt.Errorf("connect to agent: %w", err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))

	req := agentRequest{Action: "fetch_artifacts"}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, fmt.Errorf("send fetch_artifacts request: %w", err)
	}

	var resp agentResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, fmt.Errorf("read fetch_artifacts response: %w", err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("agent error: %s", resp.Error)
	}
	if resp.Artifacts == nil {
		return &FetchArtifactsResult{Artifacts: []ArtifactEntry{}}, nil
	}
	return resp.Artifacts, nil
}

func (s *Session) ConfigureGuestNetwork(ctx context.Context, req GuestNetworkRequest) error {
	configureCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	conn, err := s.Dial(configureCtx)
	if err != nil {
		return fmt.Errorf("connect to agent: %w", err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))

	payload := agentRequest{Action: "configure_guest_network", Network: &req}
	if err := json.NewEncoder(conn).Encode(payload); err != nil {
		return fmt.Errorf("send configure_guest_network request: %w", err)
	}

	var resp agentResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return fmt.Errorf("read configure_guest_network response: %w", err)
	}
	if resp.Error != "" {
		return fmt.Errorf("agent error: %s", resp.Error)
	}
	return nil
}

func (s *Session) ExportEnvironment(ctx context.Context, scope string) ([]byte, error) {
	payload, err := s.callEnvironment(ctx, "environment_export", EnvironmentRequest{Scope: scope})
	if err != nil {
		return nil, err
	}
	if payload.Archive == "" {
		return nil, fmt.Errorf("environment archive is empty")
	}
	archive, err := base64.StdEncoding.DecodeString(payload.Archive)
	if err != nil {
		return nil, fmt.Errorf("decode environment archive: %w", err)
	}
	return archive, nil
}

func (s *Session) ImportEnvironment(ctx context.Context, scope string, archive []byte) error {
	_, err := s.callEnvironment(ctx, "environment_import", EnvironmentRequest{
		Scope:   scope,
		Archive: base64.StdEncoding.EncodeToString(archive),
	})
	return err
}

func (s *Session) callEnvironment(ctx context.Context, action string, payload EnvironmentRequest) (*EnvironmentResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.TouchActivity()
	callCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	conn, err := s.Dial(callCtx)
	if err != nil {
		return nil, fmt.Errorf("connect to agent: %w", err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(2 * time.Minute))
	if err := json.NewEncoder(conn).Encode(agentRequest{Action: action, Environment: &payload}); err != nil {
		return nil, fmt.Errorf("send %s request: %w", action, err)
	}
	var resp agentResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, fmt.Errorf("read %s response: %w", action, err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("agent error: %s", resp.Error)
	}
	if resp.Environment == nil {
		return nil, fmt.Errorf("environment response missing body")
	}
	return resp.Environment, nil
}

// NewVsockDialer 创建 Firecracker vsock 连接的 Dialer。
//
// Firecracker vsock 握手协议：
//
//	HOST: CONNECT {port}\n
//	GUEST: OK {ephemeral_port}\n
func NewVsockDialer(vsockPath string, agentPort uint32) Dialer {
	return func(ctx context.Context) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, "unix", vsockPath)
		if err != nil {
			return nil, err
		}

		if _, err := fmt.Fprintf(conn, "CONNECT %d\n", agentPort); err != nil {
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

		return &vsockConn{Conn: conn, reader: reader}, nil
	}
}

// NewTCPDialer 创建 TCP 连接的 Dialer（用于 Docker 后端）。
func NewTCPDialer(addr string) Dialer {
	return func(ctx context.Context) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	}
}

// vsockConn 将 bufio.Reader（握手后可能有缓冲）和原始 Conn 合并为 net.Conn。
type vsockConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *vsockConn) Read(b []byte) (int, error) {
	return c.reader.Read(b)
}

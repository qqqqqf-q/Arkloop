package session

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"arkloop/services/sandbox/internal/firecracker"
)

// ManagerConfig 持有 Manager 所需的外部配置。
type ManagerConfig struct {
	FirecrackerBin     string
	KernelImagePath    string
	RootfsPath         string
	SocketBaseDir      string
	BootTimeoutSeconds int
	GuestAgentPort     uint32
	MaxSessions        int
}

// Manager 线程安全地管理所有活跃 Session（microVM 实例）。
type Manager struct {
	cfg      ManagerConfig
	mu       sync.Mutex
	sessions map[string]*Session
	procs    map[string]*os.Process // session_id → Firecracker 进程
}

// NewManager 创建 Session 管理器。
func NewManager(cfg ManagerConfig) *Manager {
	return &Manager{
		cfg:      cfg,
		sessions: make(map[string]*Session),
		procs:    make(map[string]*os.Process),
	}
}

// GetOrCreate 返回已有 Session；若不存在则创建并启动一个新的 microVM。
func (m *Manager) GetOrCreate(ctx context.Context, sessionID, tier string) (*Session, error) {
	if err := firecracker.ValidTier(tier); err != nil {
		return nil, err
	}

	m.mu.Lock()
	if s, ok := m.sessions[sessionID]; ok {
		m.mu.Unlock()
		return s, nil
	}
	if len(m.sessions) >= m.cfg.MaxSessions {
		m.mu.Unlock()
		return nil, fmt.Errorf("max sessions reached: %d", m.cfg.MaxSessions)
	}
	m.mu.Unlock()

	return m.create(ctx, sessionID, tier)
}

// Delete 停止并销毁指定 Session 的 microVM。
func (m *Manager) Delete(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	_, ok := m.sessions[sessionID]
	proc := m.procs[sessionID]
	if ok {
		delete(m.sessions, sessionID)
		delete(m.procs, sessionID)
	}
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	if proc != nil {
		_ = proc.Kill()
		_, _ = proc.Wait()
	}
	m.cleanSocketDir(sessionID)
	return nil
}

// CloseAll 终止所有活跃 Session，通常在服务关闭时调用。
func (m *Manager) CloseAll(ctx context.Context) {
	m.mu.Lock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	m.mu.Unlock()

	for _, id := range ids {
		_ = m.Delete(ctx, id)
	}
}

// ActiveCount 返回当前活跃 Session 数量。
func (m *Manager) ActiveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

// create 启动一个新的 Firecracker microVM 并等待 Guest Agent 就绪。
func (m *Manager) create(ctx context.Context, sessionID, tier string) (*Session, error) {
	socketDir := filepath.Join(m.cfg.SocketBaseDir, sessionID)
	if err := os.MkdirAll(socketDir, 0o700); err != nil {
		return nil, fmt.Errorf("create socket dir: %w", err)
	}

	apiSocket := filepath.Join(socketDir, "api.sock")
	vsockPath := filepath.Join(socketDir, "vsock.sock")

	// 启动 Firecracker 进程（通过 API socket 管理）
	proc, err := m.startFirecracker(ctx, apiSocket)
	if err != nil {
		_ = os.RemoveAll(socketDir)
		return nil, fmt.Errorf("start firecracker: %w", err)
	}

	// 等待 Firecracker API socket 就绪
	if err := waitForSocket(ctx, apiSocket, 5*time.Second); err != nil {
		_ = proc.Kill()
		_, _ = proc.Wait()
		_ = os.RemoveAll(socketDir)
		return nil, fmt.Errorf("firecracker api socket: %w", err)
	}

	// 通过 HTTP API 配置并启动 microVM
	tierCfg := firecracker.TierFor(tier)
	client := firecracker.NewClient(apiSocket)
	if err := client.Configure(ctx,
		firecracker.MachineConfig{VcpuCount: tierCfg.VCPUCount, MemSizeMib: tierCfg.MemSizeMiB, Smt: false},
		firecracker.BootSource{KernelImagePath: m.cfg.KernelImagePath, BootArgs: firecracker.KernelArgs},
		firecracker.Drive{DriveID: "rootfs", PathOnHost: m.cfg.RootfsPath, IsRootDevice: true, IsReadOnly: true},
		firecracker.VsockDevice{GuestCID: 3, UDSPath: vsockPath},
	); err != nil {
		_ = proc.Kill()
		_, _ = proc.Wait()
		_ = os.RemoveAll(socketDir)
		return nil, fmt.Errorf("configure microvm: %w", err)
	}
	if err := client.Start(ctx); err != nil {
		_ = proc.Kill()
		_, _ = proc.Wait()
		_ = os.RemoveAll(socketDir)
		return nil, fmt.Errorf("start microvm: %w", err)
	}

	s := &Session{
		ID:        sessionID,
		Tier:      tier,
		VsockPath: vsockPath,
		AgentPort: m.cfg.GuestAgentPort,
		CreatedAt: time.Now(),
	}

	// 等待 Guest Agent 在 vsock 端口就绪
	bootTimeout := time.Duration(m.cfg.BootTimeoutSeconds) * time.Second
	if err := waitForAgent(ctx, s, bootTimeout); err != nil {
		_ = proc.Kill()
		_, _ = proc.Wait()
		_ = os.RemoveAll(socketDir)
		return nil, fmt.Errorf("guest agent not ready: %w", err)
	}

	m.mu.Lock()
	// 双重检查：防止并发 create 竞争
	if existing, ok := m.sessions[sessionID]; ok {
		m.mu.Unlock()
		_ = proc.Kill()
		_, _ = proc.Wait()
		_ = os.RemoveAll(socketDir)
		return existing, nil
	}
	m.sessions[sessionID] = s
	m.procs[sessionID] = proc
	m.mu.Unlock()

	return s, nil
}

// startFirecracker 以非阻塞方式启动 Firecracker 进程，返回 os.Process。
// 使用 exec.Command 而非 exec.CommandContext：Firecracker 进程的生命周期由 Manager.Delete
// 显式管理，不应随请求 ctx 取消而被 SIGKILL。
func (m *Manager) startFirecracker(_ context.Context, apiSocket string) (*os.Process, error) {
	cmd := exec.Command(m.cfg.FirecrackerBin,
		"--api-sock", apiSocket,
		"--log-level", "Error",
	)
	// Firecracker 进程与 sandbox 服务解耦，stdout/stderr 丢弃（日志已由 Firecracker 内部处理）
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd.Process, nil
}

// waitForSocket 轮询等待 Unix domain socket 文件出现。
func waitForSocket(ctx context.Context, socketPath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := os.Stat(socketPath); err == nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("socket %s not ready within %s", socketPath, timeout)
}

// waitForAgent 轮询等待 Guest Agent vsock 端口就绪。
// 通过尝试发送一个 ping job 来探测（agent 启动后才能接受连接）。
func waitForAgent(ctx context.Context, s *Session, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	pingJob := ExecJob{Language: "shell", Code: "echo ready", TimeoutMs: 1000}

	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		result, err := s.Exec(probeCtx, pingJob)
		cancel()
		if err == nil && result != nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("agent not ready within %s", timeout)
}

// cleanSocketDir 移除 session 的 socket 目录（最大努力）。
func (m *Manager) cleanSocketDir(sessionID string) {
	dir := filepath.Join(m.cfg.SocketBaseDir, sessionID)
	_ = os.RemoveAll(dir)
}

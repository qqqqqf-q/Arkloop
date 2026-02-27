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
	"arkloop/services/sandbox/internal/storage"
	"arkloop/services/sandbox/internal/template"
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
	// SnapshotStore 和 Registry 是可选依赖；为 nil 时降级为纯冷启动。
	SnapshotStore storage.SnapshotStore
	Registry      *template.Registry
}

// Manager 线程安全地管理所有活跃 Session（microVM 实例）。
type Manager struct {
	cfg      ManagerConfig
	mu       sync.Mutex
	sessions map[string]*Session
	procs    map[string]*os.Process // session_id → Firecracker 进程
	creating sync.Map               // session_id → chan *createResult，防止同一 ID 并发创建
	pending  int                     // 正在创建中的 session 数量（锁内维护）
}

// NewManager 创建 Session 管理器。
func NewManager(cfg ManagerConfig) *Manager {
	return &Manager{
		cfg:      cfg,
		sessions: make(map[string]*Session),
		procs:    make(map[string]*os.Process),
	}
}

// createResult 用于同一 sessionID 并发创建时传递结果。
type createResult struct {
	session *Session
	err     error
}

// GetOrCreate 返回已有 Session；若不存在则创建并启动一个新的 microVM。
// 通过 pending 计数 + creating map 双重机制保证 MaxSessions 不被并发超额。
func (m *Manager) GetOrCreate(ctx context.Context, sessionID, tier string) (*Session, error) {
	if err := firecracker.ValidTier(tier); err != nil {
		return nil, err
	}

	m.mu.Lock()
	if s, ok := m.sessions[sessionID]; ok {
		m.mu.Unlock()
		return s, nil
	}

	// 同一 sessionID 正在创建中，等待结果
	if ch, ok := m.creating.Load(sessionID); ok {
		m.mu.Unlock()
		result := <-ch.(chan *createResult)
		if result.err != nil {
			return nil, result.err
		}
		return result.session, nil
	}

	if len(m.sessions)+m.pending >= m.cfg.MaxSessions {
		m.mu.Unlock()
		return nil, fmt.Errorf("max sessions reached: %d", m.cfg.MaxSessions)
	}

	// 预占槽位
	m.pending++
	done := make(chan *createResult, 1)
	m.creating.Store(sessionID, done)
	m.mu.Unlock()

	s, err := m.create(ctx, sessionID, tier)

	// 释放预占
	m.mu.Lock()
	m.pending--
	m.creating.Delete(sessionID)
	m.mu.Unlock()

	// 通知同一 sessionID 的其他等待者
	result := &createResult{session: s, err: err}
	done <- result
	close(done)

	return s, err
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
// 若注入了 SnapshotStore 和 Registry，优先从快照恢复；失败时降级到冷启动。
func (m *Manager) create(ctx context.Context, sessionID, tier string) (*Session, error) {
	// 查找匹配 tier 的模板（可能为空，冷启动时回退全局配置）
	var tmpl *template.Template
	if m.cfg.Registry != nil {
		if t, ok := m.cfg.Registry.ForTier(tier); ok {
			tmpl = &t
		}
	}

	// 快照恢复路径
	if m.cfg.SnapshotStore != nil && tmpl != nil {
		if s, err := m.createFromSnapshot(ctx, sessionID, tier, tmpl); err == nil {
			return s, nil
		}
		// 快照恢复失败（快照损坏或不存在），降级到冷启动
	}
	return m.createCold(ctx, sessionID, tier, tmpl)
}

// createFromSnapshot 尝试从 MinIO 快照恢复一个 microVM。
func (m *Manager) createFromSnapshot(ctx context.Context, sessionID, tier string, tmpl *template.Template) (*Session, error) {
	exists, err := m.cfg.SnapshotStore.Exists(ctx, tmpl.ID)
	if err != nil || !exists {
		return nil, fmt.Errorf("snapshot not available for %q", tmpl.ID)
	}

	socketDir := filepath.Join(m.cfg.SocketBaseDir, sessionID)
	if err := os.MkdirAll(socketDir, 0o700); err != nil {
		return nil, fmt.Errorf("create socket dir: %w", err)
	}

	apiSocket := filepath.Join(socketDir, "api.sock")
	vsockPath := filepath.Join(socketDir, "vsock.sock")

	// 下载快照到本地缓存
	memPath, diskPath, err := m.cfg.SnapshotStore.Download(ctx, tmpl.ID)
	if err != nil {
		_ = os.RemoveAll(socketDir)
		return nil, fmt.Errorf("download snapshot: %w", err)
	}

	// 启动空 Firecracker 进程（不做 Configure，直接从快照加载）
	proc, err := m.startFirecracker(ctx, apiSocket)
	if err != nil {
		_ = os.RemoveAll(socketDir)
		return nil, fmt.Errorf("start firecracker: %w", err)
	}

	if err := firecracker.WaitForSocket(ctx, apiSocket, 5*time.Second); err != nil {
		_ = proc.Kill()
		_, _ = proc.Wait()
		_ = os.RemoveAll(socketDir)
		return nil, fmt.Errorf("firecracker api socket: %w", err)
	}

	client := firecracker.NewClient(apiSocket)
	// LoadSnapshot with resumeVM=true：加载后立即恢复运行，无需额外 Patch /vm
	if err := client.LoadSnapshot(ctx, diskPath, memPath, true); err != nil {
		_ = proc.Kill()
		_, _ = proc.Wait()
		_ = os.RemoveAll(socketDir)
		return nil, fmt.Errorf("load snapshot: %w", err)
	}

	s := &Session{
		ID:        sessionID,
		Tier:      tier,
		VsockPath: vsockPath,
		AgentPort: m.cfg.GuestAgentPort,
		CreatedAt: time.Now(),
	}

	// 快照恢复后 Agent 应在数百毫秒内就绪，使用短超时以快速降级
	const snapshotAgentTimeout = 5 * time.Second
	if err := WaitForAgent(ctx, s, snapshotAgentTimeout); err != nil {
		_ = proc.Kill()
		_, _ = proc.Wait()
		_ = os.RemoveAll(socketDir)
		return nil, fmt.Errorf("guest agent not ready after snapshot restore: %w", err)
	}

	m.mu.Lock()
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

// createCold 冷启动一个新的 Firecracker microVM 并等待 Guest Agent 就绪。
// 若 tmpl 不为 nil，使用模板的 rootfs/kernel；否则使用全局配置。
func (m *Manager) createCold(ctx context.Context, sessionID, tier string, tmpl *template.Template) (*Session, error) {
	kernelPath := m.cfg.KernelImagePath
	rootfsPath := m.cfg.RootfsPath
	if tmpl != nil {
		kernelPath = tmpl.KernelImagePath
		rootfsPath = tmpl.RootfsPath
	}

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
	if err := firecracker.WaitForSocket(ctx, apiSocket, 5*time.Second); err != nil {
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
		firecracker.BootSource{KernelImagePath: kernelPath, BootArgs: firecracker.KernelArgs},
		firecracker.Drive{DriveID: "rootfs", PathOnHost: rootfsPath, IsRootDevice: true, IsReadOnly: true},
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
	if err := WaitForAgent(ctx, s, bootTimeout); err != nil {
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
	// Firecracker 自身的日志路由到宿主 stderr
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd.Process, nil
}

// WaitForAgent 轮询等待 Guest Agent vsock 端口就绪。
// 通过尝试发送一个 ping job 来探测（agent 启动后才能接受连接）。
func WaitForAgent(ctx context.Context, s *Session, timeout time.Duration) error {
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

package session

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// PoolStats 是 VMPool 的运行时统计。
type PoolStats struct {
	ReadyByTier    map[string]int
	TargetByTier   map[string]int
	TotalCreated   int64
	TotalDestroyed int64
}

// VMPool 抽象 VM 的获取与销毁，由 pool.WarmPool 实现。
type VMPool interface {
	Acquire(ctx context.Context, tier string) (*Session, *os.Process, error)
	DestroyVM(proc *os.Process, socketDir string)
	Ready() bool
	Stats() PoolStats
	Drain(ctx context.Context)
}

// ManagerConfig 持有 Manager 所需的外部配置。
type ManagerConfig struct {
	MaxSessions        int
	Pool               VMPool
	IdleTimeoutLite    int // 秒
	IdleTimeoutPro     int
	IdleTimeoutUltra   int
	MaxLifetimeSeconds int
}

// Manager 线程安全地管理所有活跃 Session（microVM 实例）。
// VM 的创建/预热由 VMPool 负责，Manager 处理 sessionID 绑定和生命周期。
type Manager struct {
	cfg      ManagerConfig
	mu       sync.Mutex
	sessions map[string]*Session
	procs    map[string]*os.Process // session_id -> Firecracker 进程
	creating sync.Map               // session_id -> chan *createResult
	pending  int

	totalReclaimed atomic.Int64
}

// NewManager 创建 Session 管理器。
func NewManager(cfg ManagerConfig) *Manager {
	return &Manager{
		cfg:      cfg,
		sessions: make(map[string]*Session),
		procs:    make(map[string]*os.Process),
	}
}

type createResult struct {
	session *Session
	err     error
}

// GetOrCreate 返回已有 Session；若不存在则从 VMPool 获取一个 VM 并绑定。
// orgID 非空时绑定到 session，已有 session 需匹配 orgID。
func (m *Manager) GetOrCreate(ctx context.Context, sessionID, tier, orgID string) (*Session, error) {
	if err := ValidTier(tier); err != nil {
		return nil, err
	}

	m.mu.Lock()
	if s, ok := m.sessions[sessionID]; ok {
		m.mu.Unlock()
		if orgID != "" && s.OrgID != "" && s.OrgID != orgID {
			return nil, fmt.Errorf("session %s: org mismatch", sessionID)
		}
		return s, nil
	}

	if ch, ok := m.creating.Load(sessionID); ok {
		m.mu.Unlock()
		result := <-ch.(chan *createResult)
		if result.err != nil {
			return nil, result.err
		}
		if orgID != "" && result.session.OrgID != "" && result.session.OrgID != orgID {
			return nil, fmt.Errorf("session %s: org mismatch", sessionID)
		}
		return result.session, nil
	}

	if len(m.sessions)+m.pending >= m.cfg.MaxSessions {
		m.mu.Unlock()
		return nil, fmt.Errorf("max sessions reached: %d", m.cfg.MaxSessions)
	}

	m.pending++
	done := make(chan *createResult, 1)
	m.creating.Store(sessionID, done)
	m.mu.Unlock()

	s, err := m.acquireAndBind(ctx, sessionID, tier, orgID)

	m.mu.Lock()
	m.pending--
	m.creating.Delete(sessionID)
	m.mu.Unlock()

	result := &createResult{session: s, err: err}
	done <- result
	close(done)

	return s, err
}

func (m *Manager) acquireAndBind(ctx context.Context, sessionID, tier, orgID string) (*Session, error) {
	s, proc, err := m.cfg.Pool.Acquire(ctx, tier)
	if err != nil {
		return nil, err
	}

	s.ID = sessionID
	s.OrgID = orgID
	s.IdleTimeout = time.Duration(m.idleTimeoutFor(tier)) * time.Second
	s.MaxLifetime = time.Duration(m.cfg.MaxLifetimeSeconds) * time.Second
	s.StartTimers(m.onSessionExpired)

	m.mu.Lock()
	if existing, ok := m.sessions[sessionID]; ok {
		m.mu.Unlock()
		s.StopTimers()
		m.cfg.Pool.DestroyVM(proc, s.SocketDir)
		return existing, nil
	}
	m.sessions[sessionID] = s
	m.procs[sessionID] = proc
	m.mu.Unlock()

	return s, nil
}

// Delete 停止并销毁指定 Session 的 microVM。
// orgID 非空时校验归属，不匹配则拒绝。
func (m *Manager) Delete(_ context.Context, sessionID, orgID string) error {
	m.mu.Lock()
	s, ok := m.sessions[sessionID]
	proc := m.procs[sessionID]
	if ok {
		if orgID != "" && s.OrgID != "" && s.OrgID != orgID {
			m.mu.Unlock()
			return fmt.Errorf("session %s: org mismatch", sessionID)
		}
		delete(m.sessions, sessionID)
		delete(m.procs, sessionID)
	}
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	s.StopTimers()
	m.cfg.Pool.DestroyVM(proc, s.SocketDir)
	return nil
}

// CloseAll 终止所有活跃 Session。服务关闭时调用。
func (m *Manager) CloseAll(ctx context.Context) {
	m.mu.Lock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	m.mu.Unlock()

	for _, id := range ids {
		_ = m.Delete(ctx, id, "")
	}
}

// ActiveCount 返回当前活跃 Session 数量。
func (m *Manager) ActiveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

// SessionsByTier 返回各 tier 的活跃 session 数量。
func (m *Manager) SessionsByTier() map[string]int {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[string]int)
	for _, s := range m.sessions {
		result[s.Tier]++
	}
	return result
}

// TotalReclaimed 返回因超时被自动回收的 session 总数。
func (m *Manager) TotalReclaimed() int64 {
	return m.totalReclaimed.Load()
}

// PoolStats 返回底层 VMPool 的运行时统计。
func (m *Manager) PoolStats() PoolStats {
	return m.cfg.Pool.Stats()
}

// PoolReady 返回底层 VMPool 是否完成初始预热。
func (m *Manager) PoolReady() bool {
	return m.cfg.Pool.Ready()
}

// DrainPool 停止底层 VMPool 的 refiller 并销毁所有预热 VM。
func (m *Manager) DrainPool(ctx context.Context) {
	m.cfg.Pool.Drain(ctx)
}

func (m *Manager) onSessionExpired(sessionID string) {
	if err := m.Delete(context.Background(), sessionID, ""); err == nil {
		m.totalReclaimed.Add(1)
	}
}

func (m *Manager) idleTimeoutFor(tier string) int {
	switch tier {
	case "pro":
		return m.cfg.IdleTimeoutPro
	case "ultra":
		return m.cfg.IdleTimeoutUltra
	default:
		return m.cfg.IdleTimeoutLite
	}
}

// WaitForAgent 轮询等待 Guest Agent vsock 端口就绪。
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



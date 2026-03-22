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

type BeforeDeleteFunc func(ctx context.Context, sn *Session, reason DeleteReason) error

type DeleteReason string

const (
	DeleteReasonExplicit    DeleteReason = "explicit"
	DeleteReasonIdleTimeout DeleteReason = "idle_timeout"
	DeleteReasonMaxLifetime DeleteReason = "max_lifetime"
	DeleteReasonShutdown    DeleteReason = "shutdown"
)

type DeleteOptions struct {
	Reason           DeleteReason
	SkipBeforeDelete bool
	IgnoreHookError  bool
}

// ManagerConfig 持有 Manager 所需的外部配置。
type ManagerConfig struct {
	MaxSessions  int
	Pool         VMPool
	IdleTimeouts map[string]int // 秒
	MaxLifetimes map[string]int // 秒
	BeforeDelete BeforeDeleteFunc
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

func (m *Manager) SetBeforeDelete(fn BeforeDeleteFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.BeforeDelete = fn
}

type createResult struct {
	session *Session
	err     error
}

// GetOrCreate 返回已有 Session；若不存在则从 VMPool 获取一个 VM 并绑定。
// accountID 非空时绑定到 session，已有 session 需匹配 accountID。
func (m *Manager) GetOrCreate(ctx context.Context, sessionID, tier, accountID string) (*Session, error) {
	if err := ValidTier(tier); err != nil {
		return nil, err
	}

	m.mu.Lock()
	if s, ok := m.sessions[sessionID]; ok {
		m.mu.Unlock()
		if accountID != "" && s.AccountID != "" && s.AccountID != accountID {
			return nil, fmt.Errorf("session %s: account mismatch", sessionID)
		}
		return s, nil
	}

	if ch, ok := m.creating.Load(sessionID); ok {
		m.mu.Unlock()
		result := <-ch.(chan *createResult)
		if result.err != nil {
			return nil, result.err
		}
		if accountID != "" && result.session.AccountID != "" && result.session.AccountID != accountID {
			return nil, fmt.Errorf("session %s: account mismatch", sessionID)
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

	s, err := m.acquireAndBind(ctx, sessionID, tier, accountID)

	m.mu.Lock()
	m.pending--
	m.creating.Delete(sessionID)
	m.mu.Unlock()

	result := &createResult{session: s, err: err}
	done <- result
	close(done)

	return s, err
}

func (m *Manager) acquireAndBind(ctx context.Context, sessionID, tier, accountID string) (*Session, error) {
	s, proc, err := m.cfg.Pool.Acquire(ctx, tier)
	if err != nil {
		return nil, err
	}

	s.ID = sessionID
	s.AccountID = accountID
	s.IdleTimeout = time.Duration(m.idleTimeoutFor(tier)) * time.Second
	s.MaxLifetime = time.Duration(m.maxLifetimeFor(tier)) * time.Second
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
// accountID 非空时校验归属，不匹配则拒绝。
func (m *Manager) Delete(ctx context.Context, sessionID, accountID string) error {
	return m.DeleteWithOptions(ctx, sessionID, accountID, DeleteOptions{Reason: DeleteReasonExplicit})
}

func (m *Manager) DeleteWithOptions(ctx context.Context, sessionID, accountID string, opts DeleteOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.Reason == "" {
		opts.Reason = DeleteReasonExplicit
	}

	m.mu.Lock()
	s, ok := m.sessions[sessionID]
	proc := m.procs[sessionID]
	if ok {
		if accountID != "" && s.AccountID != "" && s.AccountID != accountID {
			m.mu.Unlock()
			return fmt.Errorf("session %s: account mismatch", sessionID)
		}
		delete(m.sessions, sessionID)
		delete(m.procs, sessionID)
	}
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	if !opts.SkipBeforeDelete && m.cfg.BeforeDelete != nil {
		if err := m.cfg.BeforeDelete(ctx, s, opts.Reason); err != nil && !opts.IgnoreHookError {
			m.mu.Lock()
			m.sessions[sessionID] = s
			m.procs[sessionID] = proc
			m.mu.Unlock()
			return err
		}
	}

	s.StopTimers()
	m.cfg.Pool.DestroyVM(proc, s.SocketDir)
	return nil
}

func (m *Manager) DeleteSkipHook(ctx context.Context, sessionID, accountID string) error {
	return m.DeleteWithOptions(ctx, sessionID, accountID, DeleteOptions{Reason: DeleteReasonExplicit, SkipBeforeDelete: true})
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
		_ = m.DeleteWithOptions(ctx, id, "", DeleteOptions{Reason: DeleteReasonShutdown, IgnoreHookError: true})
	}
}

// ActiveCount 返回当前活跃 Session 数量。
func (m *Manager) ActiveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

// MaxSessions 返回配置的最大 Session 数量。
func (m *Manager) MaxSessions() int {
	return m.cfg.MaxSessions
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

func (m *Manager) onSessionExpired(sessionID string, reason ExpiryReason) {
	deleteReason := DeleteReasonIdleTimeout
	if reason == ExpiryReasonMaxLifetime {
		deleteReason = DeleteReasonMaxLifetime
	}
	if err := m.DeleteWithOptions(context.Background(), sessionID, "", DeleteOptions{Reason: deleteReason, IgnoreHookError: true}); err == nil {
		m.totalReclaimed.Add(1)
	}
}

func (m *Manager) idleTimeoutFor(tier string) int {
	if timeout, ok := m.cfg.IdleTimeouts[tier]; ok {
		return timeout
	}
	return m.cfg.IdleTimeouts[TierLite]
}

func (m *Manager) maxLifetimeFor(tier string) int {
	if lifetime, ok := m.cfg.MaxLifetimes[tier]; ok {
		return lifetime
	}
	return m.cfg.MaxLifetimes[TierLite]
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

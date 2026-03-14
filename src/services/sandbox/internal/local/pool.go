package local

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"arkloop/services/sandbox/internal/logging"
	"arkloop/services/sandbox/internal/session"
)

// Config holds parameters for the local pool.
type Config struct {
	Logger       *logging.JSONLogger
	WorkspaceDir string // optional base workspace directory
}

// Pool manages local agent instances for host-side ACP execution.
// Each Acquire creates an embedded TCP agent that handles ACP protocol
// requests for local processes.
type Pool struct {
	cfg            Config
	mu             sync.Mutex
	agents         map[string]*Agent // socketDir -> agent
	ready          atomic.Bool
	totalCreated   atomic.Int64
	totalDestroyed atomic.Int64
}

// New creates a LocalPool that is immediately ready.
func New(cfg Config) *Pool {
	p := &Pool{
		cfg:    cfg,
		agents: make(map[string]*Agent),
	}
	p.ready.Store(true)
	return p
}

// Acquire creates a new local agent and returns a Session with a TCP dialer.
// The os.Process return is nil (no VM to manage in local mode).
func (p *Pool) Acquire(ctx context.Context, tier string) (*session.Session, *os.Process, error) {
	id := generateID()

	agent, err := NewAgent(id)
	if err != nil {
		return nil, nil, fmt.Errorf("create local agent: %w", err)
	}

	socketDir := "local-agent-" + id

	p.mu.Lock()
	p.agents[socketDir] = agent
	p.mu.Unlock()

	s := &session.Session{
		ID:        id,
		Tier:      tier,
		Dial:      session.NewTCPDialer(agent.Addr()),
		CreatedAt: time.Now(),
		SocketDir: socketDir,
	}

	p.totalCreated.Add(1)

	if p.cfg.Logger != nil {
		p.cfg.Logger.Info("local agent acquired", logging.LogFields{},
			map[string]any{"agent_id": id, "addr": agent.Addr(), "tier": tier})
	}

	return s, nil, nil
}

// DestroyVM stops the local agent associated with socketDir.
// The proc parameter is ignored for local mode.
func (p *Pool) DestroyVM(proc *os.Process, socketDir string) {
	p.mu.Lock()
	agent, ok := p.agents[socketDir]
	if ok {
		delete(p.agents, socketDir)
	}
	p.mu.Unlock()

	if ok && agent != nil {
		_ = agent.Close()
	}
	p.totalDestroyed.Add(1)
}

// Ready returns true; the local pool requires no warm-up.
func (p *Pool) Ready() bool {
	return p.ready.Load()
}

// Stats returns runtime statistics for the local pool.
func (p *Pool) Stats() session.PoolStats {
	return session.PoolStats{
		ReadyByTier:    map[string]int{},
		TargetByTier:   map[string]int{},
		TotalCreated:   p.totalCreated.Load(),
		TotalDestroyed: p.totalDestroyed.Load(),
	}
}

// Drain closes all active agents and marks the pool as not ready.
func (p *Pool) Drain(ctx context.Context) {
	p.ready.Store(false)

	p.mu.Lock()
	snapshot := make(map[string]*Agent, len(p.agents))
	for k, v := range p.agents {
		snapshot[k] = v
	}
	p.agents = make(map[string]*Agent)
	p.mu.Unlock()

	for _, agent := range snapshot {
		_ = agent.Close()
	}
}

func generateID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "local-" + hex.EncodeToString(b)
}

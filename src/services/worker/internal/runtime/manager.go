package runtime

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	sharedtoolruntime "arkloop/services/shared/toolruntime"

	"github.com/jackc/pgx/v5/pgxpool"
)

type SnapshotBuilder func(context.Context) (sharedtoolruntime.RuntimeSnapshot, error)

type Manager struct {
	mu       sync.RWMutex
	builder  SnapshotBuilder
	ttl      time.Duration
	cached   sharedtoolruntime.RuntimeSnapshot
	cachedAt time.Time
}

func NewManager(ttl time.Duration, builder SnapshotBuilder) *Manager {
	return &Manager{ttl: ttl, builder: builder}
}

func (m *Manager) Current(ctx context.Context) (sharedtoolruntime.RuntimeSnapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if m == nil || m.builder == nil {
		return sharedtoolruntime.RuntimeSnapshot{}, nil
	}

	m.mu.RLock()
	if m.freshLocked() {
		snapshot := m.cached
		m.mu.RUnlock()
		return snapshot, nil
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.freshLocked() {
		return m.cached, nil
	}

	snapshot, err := m.builder(ctx)
	if err != nil {
		return sharedtoolruntime.RuntimeSnapshot{}, err
	}
	m.cached = snapshot
	m.cachedAt = time.Now()
	return snapshot, nil
}

func (m *Manager) Invalidate() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.cached = sharedtoolruntime.RuntimeSnapshot{}
	m.cachedAt = time.Time{}
	m.mu.Unlock()
}

func (m *Manager) StartToolProviderInvalidationListener(ctx context.Context, directPool *pgxpool.Pool) {
	if m == nil || directPool == nil || m.ttl <= 0 {
		return
	}
	go m.runToolProviderInvalidationListener(ctx, directPool)
}

func (m *Manager) freshLocked() bool {
	if m.ttl <= 0 || m.cachedAt.IsZero() {
		return false
	}
	return time.Since(m.cachedAt) < m.ttl
}

func (m *Manager) runToolProviderInvalidationListener(ctx context.Context, directPool *pgxpool.Pool) {
	const (
		baseDelay = 1 * time.Second
		maxDelay  = 30 * time.Second
	)
	delay := baseDelay
	for {
		if ctx.Err() != nil {
			return
		}
		err := m.listenToolProviderInvalidationOnce(ctx, directPool)
		if ctx.Err() != nil {
			return
		}
		slog.WarnContext(ctx, "runtime snapshot: LISTEN connection lost, retrying", "err", err, "delay", delay)
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}
	}
}

func (m *Manager) listenToolProviderInvalidationOnce(ctx context.Context, directPool *pgxpool.Pool) error {
	conn, err := directPool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "LISTEN tool_provider_config_changed"); err != nil {
		return err
	}
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		if strings.TrimSpace(n.Payload) == "" {
			continue
		}
		m.Invalidate()
	}
}

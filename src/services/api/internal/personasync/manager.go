package personasync

import (
	"context"
	"path/filepath"
	"strings"
	"sync"

	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/observability"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Manager struct {
	root        string
	pool        *pgxpool.Pool
	repo        *data.PersonasRepository
	logger      *observability.JSONLogger
	triggerCh   chan struct{}
	triggerOnce sync.Once
}

func NewManager(root string, pool *pgxpool.Pool, repo *data.PersonasRepository, logger *observability.JSONLogger) *Manager {
	return &Manager{
		root:      strings.TrimSpace(root),
		pool:      pool,
		repo:      repo,
		logger:    logger,
		triggerCh: make(chan struct{}, 1),
	}
}

func (m *Manager) SyncNow(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if m == nil {
		return nil
	}
	if m.logger != nil {
		m.logger.Info("persona_sync_skipped", observability.LogFields{}, map[string]any{
			"root": filepath.Clean(m.root),
		})
	}
	return nil
}

func (m *Manager) Trigger() {
	if m == nil {
		return
	}
	select {
	case m.triggerCh <- struct{}{}:
	default:
	}
}

func (m *Manager) Run(ctx context.Context) {
	if m == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.triggerCh:
			_ = m.SyncNow(ctx)
		}
	}
}

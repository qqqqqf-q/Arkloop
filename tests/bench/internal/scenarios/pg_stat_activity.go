package scenarios

import (
	"context"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type pgStatActivityMonitor struct {
	maxTotal  int64
	maxActive int64
	errCode   atomic.Value // string

	cancel context.CancelFunc
	done   chan struct{}
}

func startPGStatActivityMonitor(parent context.Context, dsn string) *pgStatActivityMonitor {
	cleaned := strings.TrimSpace(dsn)
	if cleaned == "" {
		return nil
	}
	if parent == nil {
		parent = context.Background()
	}

	ctx, cancel := context.WithCancel(parent)
	mon := &pgStatActivityMonitor{
		cancel: cancel,
		done:   make(chan struct{}),
	}
	mon.errCode.Store("")

	go func() {
		defer close(mon.done)

		cfg, err := pgxpool.ParseConfig(cleaned)
		if err != nil {
			mon.errCode.Store("db.invalid_dsn")
			return
		}
		cfg.MaxConns = 2

		pool, err := pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			mon.errCode.Store("db.connect_error")
			return
		}
		defer pool.Close()

		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				var total int64
				var active int64
				q := `
					SELECT
					  COUNT(*)::bigint AS total,
					  COUNT(*) FILTER (WHERE state = 'active')::bigint AS active
					FROM pg_stat_activity
					WHERE datname = current_database()
				`
				qctx, qcancel := context.WithTimeout(ctx, 500*time.Millisecond)
				err := pool.QueryRow(qctx, q).Scan(&total, &active)
				qcancel()
				if err != nil {
					continue
				}

				for {
					prev := atomic.LoadInt64(&mon.maxTotal)
					if total <= prev || atomic.CompareAndSwapInt64(&mon.maxTotal, prev, total) {
						break
					}
				}
				for {
					prev := atomic.LoadInt64(&mon.maxActive)
					if active <= prev || atomic.CompareAndSwapInt64(&mon.maxActive, prev, active) {
						break
					}
				}
			}
		}
	}()

	return mon
}

func (m *pgStatActivityMonitor) Stop() {
	if m == nil {
		return
	}
	m.cancel()
	<-m.done
}

func (m *pgStatActivityMonitor) MaxTotal() int64 {
	if m == nil {
		return 0
	}
	return atomic.LoadInt64(&m.maxTotal)
}

func (m *pgStatActivityMonitor) MaxActive() int64 {
	if m == nil {
		return 0
	}
	return atomic.LoadInt64(&m.maxActive)
}

func (m *pgStatActivityMonitor) ErrCode() string {
	if m == nil {
		return ""
	}
	raw := m.errCode.Load()
	v, _ := raw.(string)
	return strings.TrimSpace(v)
}

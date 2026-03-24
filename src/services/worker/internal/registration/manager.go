package registration

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	heartbeatInterval = 10 * time.Second
	redisTTL          = 30 * time.Second
)

// Config 是 Manager 的初始化参数。
type Config struct {
	Version        string
	Capabilities   []string
	MaxConcurrency int
}

// Manager 管理 Worker 在 DB 和 Redis 中的生命周期注册。
type Manager struct {
	workerID       uuid.UUID
	hostname       string
	version        string
	capabilities   []string
	maxConcurrency int
	currentLoad    atomic.Int32
	pool           *pgxpool.Pool
	rdb            *redis.Client
	logger         *slog.Logger
}

func NewManager(pool *pgxpool.Pool, rdb *redis.Client, cfg Config, logger *slog.Logger) (*Manager, error) {
	if pool == nil {
		return nil, fmt.Errorf("pool must not be nil")
	}
	if rdb == nil {
		return nil, fmt.Errorf("redis client must not be nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	version := cfg.Version
	if version == "" {
		version = "unknown"
	}

	caps := cfg.Capabilities
	if caps == nil {
		caps = []string{}
	}

	maxConcurrency := cfg.MaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = 4
	}

	return &Manager{
		workerID:       uuid.New(),
		hostname:       hostname,
		version:        version,
		capabilities:   caps,
		maxConcurrency: maxConcurrency,
		pool:           pool,
		rdb:            rdb,
		logger:         logger,
	}, nil
}

func (m *Manager) WorkerID() uuid.UUID {
	return m.workerID
}

func (m *Manager) Capabilities() []string {
	return m.capabilities
}

// IncrLoad 在 job 开始处理时调用。
func (m *Manager) IncrLoad() {
	m.currentLoad.Add(1)
}

// DecrLoad 在 job 处理结束后调用。
func (m *Manager) DecrLoad() {
	m.currentLoad.Add(-1)
}

// Register 在启动时写入 DB 和 Redis。
func (m *Manager) Register(ctx context.Context) error {
	capsJSON, err := json.Marshal(m.capabilities)
	if err != nil {
		return fmt.Errorf("marshal capabilities: %w", err)
	}

	_, err = m.pool.Exec(ctx,
		`INSERT INTO worker_registrations
		    (worker_id, hostname, version, status, capabilities, max_concurrency, heartbeat_at)
		 VALUES ($1, $2, $3, 'active', $4, $5, now())
		 ON CONFLICT (worker_id) DO UPDATE SET
		     hostname        = EXCLUDED.hostname,
		     version         = EXCLUDED.version,
		     status          = 'active',
		     capabilities    = EXCLUDED.capabilities,
		     max_concurrency = EXCLUDED.max_concurrency,
		     heartbeat_at    = now()`,
		m.workerID, m.hostname, m.version, capsJSON, m.maxConcurrency,
	)
	if err != nil {
		return fmt.Errorf("register db: %w", err)
	}

	if err := m.setRedis(ctx, "active"); err != nil {
		return fmt.Errorf("register redis: %w", err)
	}

	m.logger.Info("worker registered",
		"worker_id", m.workerID.String(),
		"hostname", m.hostname,
		"capabilities", m.capabilities,
	)
	return nil
}

// StartHeartbeat 启动后台心跳，ctx 取消时自动停止。
func (m *Manager) StartHeartbeat(ctx context.Context) {
	go m.heartbeatLoop(ctx)
}

func (m *Manager) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.heartbeat(ctx); err != nil {
				m.logger.Error("heartbeat failed", "error", err.Error())
			}
		}
	}
}

func (m *Manager) heartbeat(ctx context.Context) error {
	load := int(m.currentLoad.Load())
	_, err := m.pool.Exec(ctx,
		`UPDATE worker_registrations SET heartbeat_at = now(), current_load = $2 WHERE worker_id = $1`,
		m.workerID, load,
	)
	if err != nil {
		return err
	}
	pipe := m.rdb.Pipeline()
	pipe.HSet(ctx, m.redisKey(), "current_load", fmt.Sprintf("%d", load))
	pipe.Expire(ctx, m.redisKey(), redisTTL)
	_, err = pipe.Exec(ctx)
	return err
}

// Drain 标记 Worker 进入排空状态。
func (m *Manager) Drain(ctx context.Context) error {
	return m.updateStatus(ctx, "draining")
}

// MarkDead 标记 Worker 已停止并删除 Redis 注册信息。
func (m *Manager) MarkDead(ctx context.Context) error {
	if err := m.updateStatus(ctx, "dead"); err != nil {
		return err
	}
	return m.rdb.Del(ctx, m.redisKey()).Err()
}

func (m *Manager) updateStatus(ctx context.Context, status string) error {
	_, err := m.pool.Exec(ctx,
		`UPDATE worker_registrations
		 SET status = $2, heartbeat_at = now()
		 WHERE worker_id = $1`,
		m.workerID, status,
	)
	if err != nil {
		return fmt.Errorf("update status db: %w", err)
	}
	return m.setRedis(ctx, status)
}

func (m *Manager) setRedis(ctx context.Context, status string) error {
	capsJSON, _ := json.Marshal(m.capabilities)
	key := m.redisKey()

	pipe := m.rdb.Pipeline()
	pipe.HSet(ctx, key,
		"worker_id",       m.workerID.String(),
		"hostname",        m.hostname,
		"version",         m.version,
		"status",          status,
		"capabilities",    string(capsJSON),
		"max_concurrency", fmt.Sprintf("%d", m.maxConcurrency),
	)
	pipe.Expire(ctx, key, redisTTL)
	_, err := pipe.Exec(ctx)
	return err
}

func (m *Manager) redisKey() string {
	return fmt.Sprintf("arkloop:worker:%s", m.workerID.String())
}

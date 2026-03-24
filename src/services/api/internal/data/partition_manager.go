package data

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PartitionManager 定期检查并创建/清理 run_events 的月分区。
type PartitionManager struct {
	pool            *pgxpool.Pool
	logger          *slog.Logger
	interval        time.Duration
	retentionMonths int
}

func NewPartitionManager(pool *pgxpool.Pool, logger *slog.Logger) *PartitionManager {
	return &PartitionManager{
		pool:            pool,
		logger:          logger,
		interval:        24 * time.Hour,
		retentionMonths: 3,
	}
}

func NewPartitionManagerWithRetention(pool *pgxpool.Pool, logger *slog.Logger, retentionMonths int) *PartitionManager {
	if retentionMonths <= 0 {
		retentionMonths = 3
	}
	return &PartitionManager{
		pool:            pool,
		logger:          logger,
		interval:        24 * time.Hour,
		retentionMonths: retentionMonths,
	}
}

// Run 启动后台循环，定期执行 EnsurePartitions 和 DropOldPartitions。ctx 取消时退出。
func (pm *PartitionManager) Run(ctx context.Context) {
	pm.ensure(ctx)

	ticker := time.NewTicker(pm.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pm.ensure(ctx)
		}
	}
}

func (pm *PartitionManager) ensure(ctx context.Context) {
	if err := pm.EnsurePartitions(ctx); err != nil {
		pm.logger.Error("partition check failed", "error", err.Error())
	}
	if err := pm.DropOldPartitions(ctx); err != nil {
		pm.logger.Error("partition drop failed", "error", err.Error())
	}
}

// EnsurePartitions 检查并创建当前月、下月、下下月的分区（幂等）。
func (pm *PartitionManager) EnsurePartitions(ctx context.Context) error {
	now := time.Now().UTC()
	base := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	for i := 0; i < 3; i++ {
		start := base.AddDate(0, i, 0)
		end := base.AddDate(0, i+1, 0)
		name := fmt.Sprintf("run_events_p%s", start.Format("2006_01"))

		if err := pm.createPartitionIfNotExists(ctx, name, start, end); err != nil {
			return fmt.Errorf("partition %s: %w", name, err)
		}
	}
	return nil
}

// DropOldPartitions 删除超出 retentionMonths 的旧月分区。
func (pm *PartitionManager) DropOldPartitions(ctx context.Context) error {
	now := time.Now().UTC()
	cutoff := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -pm.retentionMonths, 0)

	// 查询所有 run_events 子分区
	rows, err := pm.pool.Query(ctx,
		`SELECT inhrelid::regclass::text
		 FROM pg_inherits
		 WHERE inhparent = 'run_events'::regclass`,
	)
	if err != nil {
		return fmt.Errorf("list partitions: %w", err)
	}
	defer rows.Close()

	var toDrop []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return fmt.Errorf("scan partition name: %w", err)
		}
		// 分区名格式：run_events_p2024_01
		t, err := parsePartitionMonth(name)
		if err != nil {
			continue // 不认识的分区名跳过
		}
		if t.Before(cutoff) {
			toDrop = append(toDrop, name)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("scan partitions: %w", err)
	}

	for _, name := range toDrop {
		if _, err := pm.pool.Exec(ctx, fmt.Sprintf(`DROP TABLE IF EXISTS "%s"`, name)); err != nil {
			pm.logger.Error("drop partition failed", "partition", name, "error", err.Error())
			continue
		}
		pm.logger.Info("partition dropped", "partition", name)
	}
	return nil
}

func (pm *PartitionManager) createPartitionIfNotExists(
	ctx context.Context,
	name string,
	start, end time.Time,
) error {
	ddl := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS "%s" PARTITION OF run_events FOR VALUES FROM ('%s') TO ('%s')`,
		name,
		start.Format("2006-01-02"),
		end.Format("2006-01-02"),
	)
	tag, err := pm.pool.Exec(ctx, ddl)
	if err != nil {
		return err
	}
	// CommandTag 为 "CREATE TABLE" 时表示实际创建了新分区
	if tag.String() == "CREATE TABLE" {
		pm.logger.Info("partition created", "partition", name)
	}
	return nil
}

// parsePartitionMonth 从 "run_events_p2024_01" 格式的名称解析出对应月份的起始时间。
func parsePartitionMonth(name string) (time.Time, error) {
	// 去掉 schema 前缀（如 "public."）
	bare := name
	if idx := len(name) - 1; idx > 0 {
		for i, c := range name {
			if c == '.' {
				bare = name[i+1:]
				break
			}
		}
	}

	// run_events_p2024_01 → suffix "2024_01"
	const prefix = "run_events_p"
	if len(bare) < len(prefix)+7 {
		return time.Time{}, fmt.Errorf("unrecognized partition name: %s", name)
	}
	if bare[:len(prefix)] != prefix {
		return time.Time{}, fmt.Errorf("unrecognized partition name: %s", name)
	}
	suffix := bare[len(prefix):]
	t, err := time.Parse("2006_01", suffix)
	if err != nil {
		return time.Time{}, fmt.Errorf("unrecognized partition name: %s", name)
	}
	return t, nil
}

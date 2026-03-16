//go:build !desktop

package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MemoryHitCache 是 memory.MemoryHit 的存储形式，避免 data 包依赖 memory 包。
type MemoryHitCache struct {
	URI         string  `json:"uri"`
	Abstract    string  `json:"abstract"`
	Score       float64 `json:"score"`
	MatchReason string  `json:"match_reason"`
	IsLeaf      bool    `json:"is_leaf"`
}

type MemorySnapshotRepository struct{}

// Get 读取用户记忆快照。未找到时返回 ("", false, nil)。
func (MemorySnapshotRepository) Get(ctx context.Context, pool *pgxpool.Pool, accountID, userID uuid.UUID, agentID string) (string, bool, error) {
	var block string
	err := pool.QueryRow(ctx,
		`SELECT memory_block FROM user_memory_snapshots
		 WHERE account_id = $1 AND user_id = $2 AND agent_id = $3`,
		accountID, userID, agentID,
	).Scan(&block)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return block, true, nil
}

// GetHits 读取缓存的 raw hits JSON。未找到或列为空时返回 (nil, false, nil)。
func (MemorySnapshotRepository) GetHits(ctx context.Context, pool *pgxpool.Pool, accountID, userID uuid.UUID, agentID string) ([]MemoryHitCache, bool, error) {
	var raw []byte
	err := pool.QueryRow(ctx,
		`SELECT hits_json FROM user_memory_snapshots
		 WHERE account_id = $1 AND user_id = $2 AND agent_id = $3 AND hits_json IS NOT NULL`,
		accountID, userID, agentID,
	).Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var hits []MemoryHitCache
	if err := json.Unmarshal(raw, &hits); err != nil {
		return nil, false, nil
	}
	return hits, len(hits) > 0, nil
}

// Upsert 写入或覆盖用户记忆快照。
func (MemorySnapshotRepository) Upsert(ctx context.Context, pool *pgxpool.Pool, accountID, userID uuid.UUID, agentID, memoryBlock string) error {
	_, err := pool.Exec(ctx,
		`INSERT INTO user_memory_snapshots (account_id, user_id, agent_id, memory_block, updated_at)
		 VALUES ($1, $2, $3, $4, now())
		 ON CONFLICT (account_id, user_id, agent_id)
		 DO UPDATE SET memory_block = EXCLUDED.memory_block, updated_at = now()`,
		accountID, userID, agentID, memoryBlock,
	)
	return err
}

// UpsertWithHits 同时写入渲染后的 memory_block 和原始 hits JSON。
func (MemorySnapshotRepository) UpsertWithHits(ctx context.Context, pool *pgxpool.Pool, accountID, userID uuid.UUID, agentID, memoryBlock string, hits []MemoryHitCache) error {
	hitsJSON, err := json.Marshal(hits)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO user_memory_snapshots (account_id, user_id, agent_id, memory_block, hits_json, updated_at)
		 VALUES ($1, $2, $3, $4, $5, now())
		 ON CONFLICT (account_id, user_id, agent_id)
		 DO UPDATE SET memory_block = EXCLUDED.memory_block, hits_json = EXCLUDED.hits_json, updated_at = now()`,
		accountID, userID, agentID, memoryBlock, hitsJSON,
	)
	return err
}

// AppendMemoryLine 原子追加一条 memory 行，避免并发写互相覆盖。
func (MemorySnapshotRepository) AppendMemoryLine(ctx context.Context, pool *pgxpool.Pool, accountID, userID uuid.UUID, agentID, line string) error {
	if pool == nil {
		return fmt.Errorf("snapshot pool must not be nil")
	}
	cleanedLine := strings.TrimSpace(line)
	if cleanedLine == "" {
		return fmt.Errorf("snapshot line must not be empty")
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var block string
	err = tx.QueryRow(ctx,
		`SELECT memory_block FROM user_memory_snapshots
		 WHERE account_id = $1 AND user_id = $2 AND agent_id = $3
		 FOR UPDATE`,
		accountID, userID, agentID,
	).Scan(&block)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		tag, execErr := tx.Exec(ctx,
			`INSERT INTO user_memory_snapshots (account_id, user_id, agent_id, memory_block, updated_at)
			 VALUES ($1, $2, $3, $4, now())
			 ON CONFLICT DO NOTHING`,
			accountID, userID, agentID, newMemoryBlock(cleanedLine),
		)
		if execErr != nil {
			return execErr
		}
		if tag.RowsAffected() == 0 {
			err = tx.QueryRow(ctx,
				`SELECT memory_block FROM user_memory_snapshots
				 WHERE account_id = $1 AND user_id = $2 AND agent_id = $3
				 FOR UPDATE`,
				accountID, userID, agentID,
			).Scan(&block)
			if err != nil {
				return err
			}
			updatedBlock := appendMemoryLineToBlock(block, cleanedLine)
			if _, err := tx.Exec(ctx,
				`UPDATE user_memory_snapshots
				 SET memory_block = $4, updated_at = now()
				 WHERE account_id = $1 AND user_id = $2 AND agent_id = $3`,
				accountID, userID, agentID, updatedBlock,
			); err != nil {
				return err
			}
		}
		return tx.Commit(ctx)
	}

	updatedBlock := appendMemoryLineToBlock(block, cleanedLine)
	if _, err := tx.Exec(ctx,
		`UPDATE user_memory_snapshots
		 SET memory_block = $4, updated_at = now()
		 WHERE account_id = $1 AND user_id = $2 AND agent_id = $3`,
		accountID, userID, agentID, updatedBlock,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func newMemoryBlock(line string) string {
	return "\n\n<memory>\n- " + strings.TrimSpace(line) + "\n</memory>"
}

func appendMemoryLineToBlock(block, line string) string {
	cleanedLine := strings.TrimSpace(line)
	cleanedBlock := strings.TrimSpace(block)
	if cleanedLine == "" {
		return block
	}
	if cleanedBlock == "" {
		return newMemoryBlock(cleanedLine)
	}
	if strings.Contains(block, "</memory>") {
		return strings.Replace(block, "</memory>", "- "+cleanedLine+"\n</memory>", 1)
	}
	if strings.Contains(block, "<memory>") {
		if strings.HasSuffix(block, "\n") {
			return block + "- " + cleanedLine + "\n</memory>"
		}
		return block + "\n- " + cleanedLine + "\n</memory>"
	}
	return newMemoryBlock(cleanedLine)
}

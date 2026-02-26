package data

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

type ThreadStarRepository struct {
	db Querier
}

func NewThreadStarRepository(db Querier) (*ThreadStarRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &ThreadStarRepository{db: db}, nil
}

// Star 收藏一个 thread，已收藏时幂等忽略。
func (r *ThreadStarRepository) Star(ctx context.Context, userID, threadID uuid.UUID) error {
	if ctx == nil {
		ctx = context.Background()
	}
	_, err := r.db.Exec(
		ctx,
		`INSERT INTO thread_stars (user_id, thread_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		userID, threadID,
	)
	return err
}

// Unstar 取消收藏，未收藏时幂等忽略。
func (r *ThreadStarRepository) Unstar(ctx context.Context, userID, threadID uuid.UUID) error {
	if ctx == nil {
		ctx = context.Background()
	}
	_, err := r.db.Exec(
		ctx,
		`DELETE FROM thread_stars WHERE user_id = $1 AND thread_id = $2`,
		userID, threadID,
	)
	return err
}

// ListByUser 返回指定用户收藏的所有 thread ID 列表。
func (r *ThreadStarRepository) ListByUser(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := r.db.Query(
		ctx,
		`SELECT thread_id FROM thread_stars WHERE user_id = $1 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

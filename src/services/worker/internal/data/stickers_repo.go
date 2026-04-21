package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type AccountSticker struct {
	ID                uuid.UUID
	AccountID         uuid.UUID
	ContentHash       string
	StorageKey        string
	PreviewStorageKey string
	FileSize          int64
	MimeType          string
	IsAnimated        bool
	ShortTags         string
	LongDesc          string
	UsageCount        int
	LastUsedAt        *time.Time
	IsRegistered      bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type StickerDescriptionCache struct {
	ContentHash string
	Description string
	EmotionTags string
	Timestamp   time.Time
}

type AccountStickersRepository struct{}

func (AccountStickersRepository) GetByHash(ctx context.Context, db QueryDB, accountID uuid.UUID, contentHash string) (*AccountSticker, error) {
	if db == nil {
		return nil, fmt.Errorf("db must not be nil")
	}
	var item AccountSticker
	err := db.QueryRow(ctx, `
		SELECT id, account_id, content_hash, storage_key, preview_storage_key, file_size, mime_type,
		       is_animated, short_tags, long_desc, usage_count, last_used_at, is_registered, created_at, updated_at
		  FROM account_stickers
		 WHERE account_id = $1
		   AND content_hash = $2`,
		accountID, strings.TrimSpace(contentHash),
	).Scan(
		&item.ID,
		&item.AccountID,
		&item.ContentHash,
		&item.StorageKey,
		&item.PreviewStorageKey,
		&item.FileSize,
		&item.MimeType,
		&item.IsAnimated,
		&item.ShortTags,
		&item.LongDesc,
		&item.UsageCount,
		&item.LastUsedAt,
		&item.IsRegistered,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func (AccountStickersRepository) MarkRegistered(ctx context.Context, db DB, accountID uuid.UUID, contentHash, description, tags string) error {
	if db == nil {
		return fmt.Errorf("db must not be nil")
	}
	now := time.Now().UTC()
	_, err := db.Exec(ctx, `
		UPDATE account_stickers
		   SET long_desc = $3,
		       short_tags = $4,
		       is_registered = TRUE,
		       updated_at = $5
		 WHERE account_id = $1
		   AND content_hash = $2`,
		accountID,
		strings.TrimSpace(contentHash),
		strings.TrimSpace(description),
		strings.TrimSpace(tags),
		now,
	)
	return err
}

func (AccountStickersRepository) ListHot(ctx context.Context, db QueryDB, accountID uuid.UUID, usedSince time.Time, limit int) ([]AccountSticker, error) {
	if db == nil {
		return nil, fmt.Errorf("db must not be nil")
	}
	if limit <= 0 {
		limit = 20
	}
	rows, err := db.Query(ctx, `
		SELECT id, account_id, content_hash, storage_key, preview_storage_key, file_size, mime_type,
		       is_animated, short_tags, long_desc, usage_count, last_used_at, is_registered, created_at, updated_at
		  FROM account_stickers
		 WHERE account_id = $1
		   AND is_registered = TRUE
		   AND last_used_at IS NOT NULL
		   AND last_used_at >= $2
		 ORDER BY usage_count DESC, last_used_at DESC
		 LIMIT $3`,
		accountID, usedSince.UTC(), limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AccountSticker, 0, limit)
	for rows.Next() {
		item, scanErr := scanAccountSticker(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (AccountStickersRepository) Search(ctx context.Context, db QueryDB, accountID uuid.UUID, query string, limit int) ([]AccountSticker, error) {
	if db == nil {
		return nil, fmt.Errorf("db must not be nil")
	}
	if limit <= 0 {
		limit = 10
	}
	pattern := "%" + strings.ToLower(strings.TrimSpace(query)) + "%"
	rows, err := db.Query(ctx, `
		SELECT id, account_id, content_hash, storage_key, preview_storage_key, file_size, mime_type,
		       is_animated, short_tags, long_desc, usage_count, last_used_at, is_registered, created_at, updated_at
		  FROM account_stickers
		 WHERE account_id = $1
		   AND is_registered = TRUE
		   AND (
		   	LOWER(short_tags) LIKE $2
		   	OR LOWER(long_desc) LIKE $2
		   )
		 ORDER BY usage_count DESC, last_used_at DESC
		 LIMIT $3`,
		accountID, pattern, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AccountSticker, 0, limit)
	for rows.Next() {
		item, scanErr := scanAccountSticker(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (AccountStickersRepository) ListRegistered(ctx context.Context, db QueryDB, accountID uuid.UUID, limit int) ([]AccountSticker, error) {
	if db == nil {
		return nil, fmt.Errorf("db must not be nil")
	}
	if limit <= 0 {
		limit = 20
	}
	rows, err := db.Query(ctx, `
		SELECT id, account_id, content_hash, storage_key, preview_storage_key, file_size, mime_type,
		       is_animated, short_tags, long_desc, usage_count, last_used_at, is_registered, created_at, updated_at
		  FROM account_stickers
		 WHERE account_id = $1
		   AND is_registered = TRUE
		 ORDER BY usage_count DESC, last_used_at DESC NULLS LAST, created_at DESC
		 LIMIT $2`,
		accountID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AccountSticker, 0, limit)
	for rows.Next() {
		item, scanErr := scanAccountSticker(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (AccountStickersRepository) IncrementUsage(ctx context.Context, db DB, accountID uuid.UUID, contentHash string) error {
	if db == nil {
		return fmt.Errorf("db must not be nil")
	}
	now := time.Now().UTC()
	_, err := db.Exec(ctx, `
		UPDATE account_stickers
		   SET usage_count = usage_count + 1,
		       last_used_at = $3,
		       updated_at = $3
		 WHERE account_id = $1
		   AND content_hash = $2`,
		accountID, strings.TrimSpace(contentHash), now,
	)
	return err
}

type StickerDescriptionCacheRepository struct{}

func (StickerDescriptionCacheRepository) Get(ctx context.Context, db QueryDB, contentHash string) (*StickerDescriptionCache, error) {
	if db == nil {
		return nil, fmt.Errorf("db must not be nil")
	}
	var item StickerDescriptionCache
	err := db.QueryRow(ctx, `
		SELECT content_hash, description, emotion_tags, timestamp
		  FROM sticker_description_cache
		 WHERE content_hash = $1`,
		strings.TrimSpace(contentHash),
	).Scan(&item.ContentHash, &item.Description, &item.EmotionTags, &item.Timestamp)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func (StickerDescriptionCacheRepository) Upsert(ctx context.Context, db DB, contentHash, description, tags string) error {
	if db == nil {
		return fmt.Errorf("db must not be nil")
	}
	now := time.Now().UTC()
	_, err := db.Exec(ctx, `
		INSERT INTO sticker_description_cache (content_hash, description, emotion_tags, timestamp)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (content_hash) DO UPDATE SET
			description = EXCLUDED.description,
			emotion_tags = EXCLUDED.emotion_tags,
			timestamp = EXCLUDED.timestamp`,
		strings.TrimSpace(contentHash),
		strings.TrimSpace(description),
		strings.TrimSpace(tags),
		now,
	)
	return err
}

func scanAccountSticker(row interface{ Scan(dest ...any) error }) (AccountSticker, error) {
	var item AccountSticker
	err := row.Scan(
		&item.ID,
		&item.AccountID,
		&item.ContentHash,
		&item.StorageKey,
		&item.PreviewStorageKey,
		&item.FileSize,
		&item.MimeType,
		&item.IsAnimated,
		&item.ShortTags,
		&item.LongDesc,
		&item.UsageCount,
		&item.LastUsedAt,
		&item.IsRegistered,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

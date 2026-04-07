package data

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type AccountSettingsQueryer interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type AccountSettingsRepository struct {
	db AccountSettingsQueryer
}

func NewAccountSettingsRepository(db AccountSettingsQueryer) *AccountSettingsRepository {
	if db == nil {
		return nil
	}
	return &AccountSettingsRepository{db: db}
}

func (r *AccountSettingsRepository) PipelineTraceEnabled(ctx context.Context, accountID uuid.UUID) (bool, error) {
	if r == nil || r.db == nil || accountID == uuid.Nil {
		return false, nil
	}
	var raw any
	if err := r.db.QueryRow(ctx,
		`SELECT settings_json
		   FROM accounts
		  WHERE id = $1
		  LIMIT 1`,
		accountID,
	).Scan(&raw); err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	normalized, err := normalizeSettingsJSON(raw)
	if err != nil || len(normalized) == 0 {
		return false, nil
	}
	var parsed map[string]any
	if err := json.Unmarshal(normalized, &parsed); err != nil {
		return false, nil
	}
	value, ok := parsed["pipeline_trace_enabled"].(bool)
	return ok && value, nil
}

func normalizeSettingsJSON(raw any) ([]byte, error) {
	switch value := raw.(type) {
	case nil:
		return nil, nil
	case []byte:
		return value, nil
	case string:
		return []byte(value), nil
	default:
		return nil, fmt.Errorf("unsupported settings_json type %T", raw)
	}
}

package data

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ToolDescriptionOverride struct {
	ProjectID   *uuid.UUID
	Scope       string
	ToolName    string
	Description string
	IsDisabled  bool
	UpdatedAt   time.Time
}

type toolDescriptionOverrideQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type ToolDescriptionOverridesRepository struct {
	db toolDescriptionOverrideQuerier
}

func NewToolDescriptionOverridesRepository(db toolDescriptionOverrideQuerier) (*ToolDescriptionOverridesRepository, error) {
	if db == nil {
		return nil, fmt.Errorf("db must not be nil")
	}
	return &ToolDescriptionOverridesRepository{db: db}, nil
}

func (r *ToolDescriptionOverridesRepository) ListByScope(ctx context.Context, projectID *uuid.UUID, scope string) ([]ToolDescriptionOverride, error) {
	if scope != "project" && scope != "platform" {
		return nil, fmt.Errorf("scope must be project or platform")
	}

	var query string
	var args []any
	if scope == "platform" {
		query = `
			SELECT project_id, scope, tool_name, description, is_disabled, updated_at
			FROM tool_description_overrides
			WHERE project_id IS NULL AND scope = 'platform'
			ORDER BY tool_name ASC`
	} else {
		query = `
			SELECT project_id, scope, tool_name, description, is_disabled, updated_at
			FROM tool_description_overrides
			WHERE project_id = $1 AND scope = 'project'
			ORDER BY tool_name ASC`
		args = append(args, projectID)
	}

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]ToolDescriptionOverride, 0)
	for rows.Next() {
		var override ToolDescriptionOverride
		if err := rows.Scan(&override.ProjectID, &override.Scope, &override.ToolName, &override.Description, &override.IsDisabled, &override.UpdatedAt); err != nil {
			return nil, err
		}
		override.ToolName = strings.TrimSpace(override.ToolName)
		override.Description = strings.TrimSpace(override.Description)
		out = append(out, override)
	}
	return out, rows.Err()
}

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
ID          uuid.UUID
ToolName    string
Description string
IsDisabled  bool
UpdatedAt   time.Time
}

type ToolDescriptionOverridesRepository struct {
db Querier
}

func NewToolDescriptionOverridesRepository(db Querier) (*ToolDescriptionOverridesRepository, error) {
if db == nil {
return nil, fmt.Errorf("db must not be nil")
}
return &ToolDescriptionOverridesRepository{db: db}, nil
}

func (r *ToolDescriptionOverridesRepository) List(ctx context.Context) ([]ToolDescriptionOverride, error) {
rows, err := r.db.Query(ctx, `
SELECT id, tool_name, description, is_disabled, updated_at
FROM tool_description_overrides
ORDER BY tool_name ASC
`)
if err != nil {
return nil, err
}
defer rows.Close()

var out []ToolDescriptionOverride
for rows.Next() {
var o ToolDescriptionOverride
if err := rows.Scan(&o.ID, &o.ToolName, &o.Description, &o.IsDisabled, &o.UpdatedAt); err != nil {
return nil, err
}
out = append(out, o)
}
return out, rows.Err()
}

func (r *ToolDescriptionOverridesRepository) Upsert(ctx context.Context, toolName string, description string) error {
name := strings.TrimSpace(toolName)
if name == "" {
return fmt.Errorf("tool_name must not be empty")
}
desc := strings.TrimSpace(description)
if desc == "" {
return fmt.Errorf("description must not be empty")
}

_, err := r.db.Exec(ctx, `
INSERT INTO tool_description_overrides (tool_name, description, updated_at)
VALUES ($1, $2, now())
ON CONFLICT (tool_name)
DO UPDATE SET description = EXCLUDED.description, updated_at = now()
`, name, desc)
return err
}

func (r *ToolDescriptionOverridesRepository) Delete(ctx context.Context, toolName string) error {
name := strings.TrimSpace(toolName)
if name == "" {
return fmt.Errorf("tool_name must not be empty")
}

tag, err := r.db.Exec(ctx, `
UPDATE tool_description_overrides
SET description = '', updated_at = now()
WHERE tool_name = $1 AND is_disabled = TRUE
`, name)
if err != nil {
return err
}
if tag.RowsAffected() > 0 {
return nil
}

tag, err = r.db.Exec(ctx, `
DELETE FROM tool_description_overrides WHERE tool_name = $1
`, name)
if err != nil {
return err
}
if tag.RowsAffected() == 0 {
return pgx.ErrNoRows
}
return nil
}

func (r *ToolDescriptionOverridesRepository) SetDisabled(ctx context.Context, toolName string, disabled bool) error {
name := strings.TrimSpace(toolName)
if name == "" {
return fmt.Errorf("tool_name must not be empty")
}

if disabled {
_, err := r.db.Exec(ctx, `
INSERT INTO tool_description_overrides (tool_name, description, is_disabled, updated_at)
VALUES ($1, '', TRUE, now())
ON CONFLICT (tool_name)
DO UPDATE SET is_disabled = TRUE, updated_at = now()
`, name)
return err
}

tag, err := r.db.Exec(ctx, `
UPDATE tool_description_overrides
SET is_disabled = FALSE, updated_at = now()
WHERE tool_name = $1 AND description <> ''
`, name)
if err != nil {
return err
}
if tag.RowsAffected() > 0 {
return nil
}
_, err = r.db.Exec(ctx, `
DELETE FROM tool_description_overrides WHERE tool_name = $1
`, name)
return err
}

package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type PromptTemplate struct {
	ID          uuid.UUID
	OrgID       uuid.UUID
	Name        string
	Content     string
	Variables   []string
	IsDefault   bool
	Version     int
	PublishedAt *time.Time
	CreatedAt   time.Time
}

type PromptTemplateUpdateFields struct {
	SetName      bool
	Name         string
	SetContent   bool
	Content      string
	SetIsDefault bool
	IsDefault    bool
}

type PromptTemplateRepository struct {
	db Querier
}

func NewPromptTemplateRepository(db Querier) (*PromptTemplateRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &PromptTemplateRepository{db: db}, nil
}

func (r *PromptTemplateRepository) Create(
	ctx context.Context,
	orgID uuid.UUID,
	name string,
	content string,
	variables []string,
	isDefault bool,
) (PromptTemplate, error) {
	if orgID == uuid.Nil {
		return PromptTemplate{}, fmt.Errorf("prompt_templates: org_id must not be empty")
	}
	if name == "" {
		return PromptTemplate{}, fmt.Errorf("prompt_templates: name must not be empty")
	}
	if variables == nil {
		variables = []string{}
	}

	var pt PromptTemplate
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO prompt_templates (org_id, name, content, variables, is_default)
		 VALUES ($1, $2, $3, $4::jsonb, $5)
		 RETURNING id, org_id, name, content, variables, is_default, version, published_at, created_at`,
		orgID, name, content, variablesToJSON(variables), isDefault,
	).Scan(
		&pt.ID, &pt.OrgID, &pt.Name, &pt.Content,
		&pt.Variables, &pt.IsDefault, &pt.Version, &pt.PublishedAt, &pt.CreatedAt,
	)
	if err != nil {
		return PromptTemplate{}, fmt.Errorf("prompt_templates.Create: %w", err)
	}
	return pt, nil
}

func (r *PromptTemplateRepository) GetByID(ctx context.Context, id uuid.UUID) (*PromptTemplate, error) {
	var pt PromptTemplate
	err := r.db.QueryRow(
		ctx,
		`SELECT id, org_id, name, content, variables, is_default, version, published_at, created_at
		 FROM prompt_templates WHERE id = $1`,
		id,
	).Scan(
		&pt.ID, &pt.OrgID, &pt.Name, &pt.Content,
		&pt.Variables, &pt.IsDefault, &pt.Version, &pt.PublishedAt, &pt.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("prompt_templates.GetByID: %w", err)
	}
	return &pt, nil
}

func (r *PromptTemplateRepository) ListByOrg(ctx context.Context, orgID uuid.UUID) ([]PromptTemplate, error) {
	rows, err := r.db.Query(
		ctx,
		`SELECT id, org_id, name, content, variables, is_default, version, published_at, created_at
		 FROM prompt_templates
		 WHERE org_id = $1
		 ORDER BY created_at ASC`,
		orgID,
	)
	if err != nil {
		return nil, fmt.Errorf("prompt_templates.ListByOrg: %w", err)
	}
	defer rows.Close()

	var templates []PromptTemplate
	for rows.Next() {
		var pt PromptTemplate
		if err := rows.Scan(
			&pt.ID, &pt.OrgID, &pt.Name, &pt.Content,
			&pt.Variables, &pt.IsDefault, &pt.Version, &pt.PublishedAt, &pt.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("prompt_templates.ListByOrg scan: %w", err)
		}
		templates = append(templates, pt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("prompt_templates.ListByOrg rows: %w", err)
	}
	return templates, nil
}

func (r *PromptTemplateRepository) Update(ctx context.Context, id uuid.UUID, orgID uuid.UUID, fields PromptTemplateUpdateFields) (*PromptTemplate, error) {
	if !fields.SetName && !fields.SetContent && !fields.SetIsDefault {
		return nil, fmt.Errorf("prompt_templates.Update: no fields to update")
	}

	var pt PromptTemplate
	err := r.db.QueryRow(
		ctx,
		`UPDATE prompt_templates
		 SET name       = CASE WHEN $3 THEN $4 ELSE name END,
		     content    = CASE WHEN $5 THEN $6 ELSE content END,
		     is_default = CASE WHEN $7 THEN $8 ELSE is_default END
		 WHERE id = $1 AND org_id = $2
		 RETURNING id, org_id, name, content, variables, is_default, version, published_at, created_at`,
		id, orgID,
		fields.SetName, fields.Name,
		fields.SetContent, fields.Content,
		fields.SetIsDefault, fields.IsDefault,
	).Scan(
		&pt.ID, &pt.OrgID, &pt.Name, &pt.Content,
		&pt.Variables, &pt.IsDefault, &pt.Version, &pt.PublishedAt, &pt.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("prompt_templates.Update: %w", err)
	}
	return &pt, nil
}

// variablesToJSON 将字符串切片序列化为 JSON 数组字符串，供 JSONB 列使用。
func variablesToJSON(vars []string) string {
	if len(vars) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(vars)
	return string(b)
}

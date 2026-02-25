package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// WithTx 返回一个使用给定事务的 LlmRoutesRepository 副本。
func (r *LlmRoutesRepository) WithTx(tx pgx.Tx) *LlmRoutesRepository {
	return &LlmRoutesRepository{db: tx}
}

type LlmRoute struct {
	ID              uuid.UUID
	OrgID           uuid.UUID
	CredentialID    uuid.UUID
	Model           string
	Priority        int
	IsDefault       bool
	WhenJSON        json.RawMessage
	Multiplier      float64
	CostPer1kInput  *float64
	CostPer1kOutput *float64
	CreatedAt       time.Time
}

type LlmRoutesRepository struct {
	db Querier
}

func NewLlmRoutesRepository(db Querier) (*LlmRoutesRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &LlmRoutesRepository{db: db}, nil
}

// Create 为指定凭证创建路由规则。
func (r *LlmRoutesRepository) Create(
	ctx context.Context,
	orgID uuid.UUID,
	credentialID uuid.UUID,
	model string,
	priority int,
	isDefault bool,
	whenJSON json.RawMessage,
	multiplier float64,
	costPer1kInput *float64,
	costPer1kOutput *float64,
) (LlmRoute, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if orgID == uuid.Nil {
		return LlmRoute{}, fmt.Errorf("org_id must not be nil")
	}
	if credentialID == uuid.Nil {
		return LlmRoute{}, fmt.Errorf("credential_id must not be nil")
	}
	if strings.TrimSpace(model) == "" {
		return LlmRoute{}, fmt.Errorf("model must not be empty")
	}

	if len(whenJSON) == 0 {
		whenJSON = json.RawMessage("{}")
	}
	if multiplier <= 0 {
		multiplier = 1.0
	}

	var route LlmRoute
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO llm_routes (org_id, credential_id, model, priority, is_default, when_json, multiplier, cost_per_1k_input, cost_per_1k_output)
		 VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9)
		 RETURNING id, org_id, credential_id, model, priority, is_default, when_json, multiplier, cost_per_1k_input, cost_per_1k_output, created_at`,
		orgID, credentialID, model, priority, isDefault, string(whenJSON), multiplier, costPer1kInput, costPer1kOutput,
	).Scan(
		&route.ID, &route.OrgID, &route.CredentialID, &route.Model,
		&route.Priority, &route.IsDefault, &route.WhenJSON,
		&route.Multiplier, &route.CostPer1kInput, &route.CostPer1kOutput,
		&route.CreatedAt,
	)
	if err != nil {
		return LlmRoute{}, err
	}
	return route, nil
}

// ListByCredential 返回指定凭证的所有路由，按 priority 降序。
func (r *LlmRoutesRepository) ListByCredential(ctx context.Context, orgID, credentialID uuid.UUID) ([]LlmRoute, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT id, org_id, credential_id, model, priority, is_default, when_json, multiplier, cost_per_1k_input, cost_per_1k_output, created_at
		 FROM llm_routes
		 WHERE org_id = $1 AND credential_id = $2
		 ORDER BY priority DESC`,
		orgID, credentialID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	routes := []LlmRoute{}
	for rows.Next() {
		var route LlmRoute
		if err := rows.Scan(
			&route.ID, &route.OrgID, &route.CredentialID, &route.Model,
			&route.Priority, &route.IsDefault, &route.WhenJSON,
			&route.Multiplier, &route.CostPer1kInput, &route.CostPer1kOutput,
			&route.CreatedAt,
		); err != nil {
			return nil, err
		}
		routes = append(routes, route)
	}
	return routes, rows.Err()
}

// ListByOrg 返回 org 下所有路由，按 priority 降序。
func (r *LlmRoutesRepository) ListByOrg(ctx context.Context, orgID uuid.UUID) ([]LlmRoute, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT r.id, r.org_id, r.credential_id, r.model, r.priority, r.is_default, r.when_json, r.multiplier, r.cost_per_1k_input, r.cost_per_1k_output, r.created_at
		 FROM llm_routes r
		 JOIN llm_credentials c ON c.id = r.credential_id
		 WHERE r.org_id = $1 AND c.revoked_at IS NULL
		 ORDER BY r.priority DESC`,
		orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	routes := []LlmRoute{}
	for rows.Next() {
		var route LlmRoute
		if err := rows.Scan(
			&route.ID, &route.OrgID, &route.CredentialID, &route.Model,
			&route.Priority, &route.IsDefault, &route.WhenJSON,
			&route.Multiplier, &route.CostPer1kInput, &route.CostPer1kOutput,
			&route.CreatedAt,
		); err != nil {
			return nil, err
		}
		routes = append(routes, route)
	}
	return routes, rows.Err()
}

// ListAllActive 返回所有 org 中关联未吊销凭证的路由（供 Worker 启动时加载）。
func (r *LlmRoutesRepository) ListAllActive(ctx context.Context) ([]LlmRoute, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	rows, err := r.db.Query(
		ctx,
		`SELECT r.id, r.org_id, r.credential_id, r.model, r.priority, r.is_default, r.when_json, r.multiplier, r.cost_per_1k_input, r.cost_per_1k_output, r.created_at
		 FROM llm_routes r
		 JOIN llm_credentials c ON c.id = r.credential_id
		 WHERE c.revoked_at IS NULL
		 ORDER BY r.priority DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	routes := []LlmRoute{}
	for rows.Next() {
		var route LlmRoute
		if err := rows.Scan(
			&route.ID, &route.OrgID, &route.CredentialID, &route.Model,
			&route.Priority, &route.IsDefault, &route.WhenJSON,
			&route.Multiplier, &route.CostPer1kInput, &route.CostPer1kOutput,
			&route.CreatedAt,
		); err != nil {
			return nil, err
		}
		routes = append(routes, route)
	}
	return routes, rows.Err()
}

// GetByID 按 ID 查询，找不到返回 nil。
func (r *LlmRoutesRepository) GetByID(ctx context.Context, orgID, id uuid.UUID) (*LlmRoute, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var route LlmRoute
	err := r.db.QueryRow(
		ctx,
		`SELECT id, org_id, credential_id, model, priority, is_default, when_json, multiplier, cost_per_1k_input, cost_per_1k_output, created_at
		 FROM llm_routes
		 WHERE id = $1 AND org_id = $2`,
		id, orgID,
	).Scan(
		&route.ID, &route.OrgID, &route.CredentialID, &route.Model,
		&route.Priority, &route.IsDefault, &route.WhenJSON,
		&route.Multiplier, &route.CostPer1kInput, &route.CostPer1kOutput,
		&route.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &route, nil
}

// Update 更新路由的可变字段。
func (r *LlmRoutesRepository) Update(
	ctx context.Context,
	orgID uuid.UUID,
	routeID uuid.UUID,
	model string,
	priority int,
	isDefault bool,
	whenJSON json.RawMessage,
	multiplier float64,
	costPer1kInput *float64,
	costPer1kOutput *float64,
) (LlmRoute, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(model) == "" {
		return LlmRoute{}, fmt.Errorf("model must not be empty")
	}
	if len(whenJSON) == 0 {
		whenJSON = json.RawMessage("{}")
	}
	if multiplier <= 0 {
		multiplier = 1.0
	}

	var route LlmRoute
	err := r.db.QueryRow(
		ctx,
		`UPDATE llm_routes
		 SET model = $3, priority = $4, is_default = $5, when_json = $6::jsonb,
		     multiplier = $7, cost_per_1k_input = $8, cost_per_1k_output = $9
		 WHERE id = $1 AND org_id = $2
		 RETURNING id, org_id, credential_id, model, priority, is_default, when_json, multiplier, cost_per_1k_input, cost_per_1k_output, created_at`,
		routeID, orgID, model, priority, isDefault, string(whenJSON), multiplier, costPer1kInput, costPer1kOutput,
	).Scan(
		&route.ID, &route.OrgID, &route.CredentialID, &route.Model,
		&route.Priority, &route.IsDefault, &route.WhenJSON,
		&route.Multiplier, &route.CostPer1kInput, &route.CostPer1kOutput,
		&route.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return LlmRoute{}, fmt.Errorf("route not found")
		}
		return LlmRoute{}, err
	}
	return route, nil
}

// DeleteByCredential 删除凭证的所有路由。
func (r *LlmRoutesRepository) DeleteByCredential(ctx context.Context, orgID, credentialID uuid.UUID) error {
	if ctx == nil {
		ctx = context.Background()
	}

	_, err := r.db.Exec(
		ctx,
		`DELETE FROM llm_routes WHERE credential_id = $1 AND org_id = $2`,
		credentialID, orgID,
	)
	return err
}

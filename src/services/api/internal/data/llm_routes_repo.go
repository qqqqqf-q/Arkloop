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
	"github.com/jackc/pgx/v5/pgconn"
)

// WithTx 返回一个使用给定事务的 LlmRoutesRepository 副本。
func (r *LlmRoutesRepository) WithTx(tx pgx.Tx) *LlmRoutesRepository {
	return &LlmRoutesRepository{db: tx}
}

type LlmRoute struct {
	ID                  uuid.UUID
	OrgID               uuid.UUID
	CredentialID        uuid.UUID
	Model               string
	Priority            int
	IsDefault           bool
	Tags                []string
	WhenJSON            json.RawMessage
	Multiplier          float64
	CostPer1kInput      *float64
	CostPer1kOutput     *float64
	CostPer1kCacheWrite *float64
	CostPer1kCacheRead  *float64
	CreatedAt           time.Time
}

type LlmRouteModelConflictError struct {
	CredentialID uuid.UUID
	Model        string
}

func (e LlmRouteModelConflictError) Error() string {
	return fmt.Sprintf("llm route model %q already exists for credential %s", e.Model, e.CredentialID)
}

type CreateLlmRouteParams struct {
	OrgID               uuid.UUID
	CredentialID        uuid.UUID
	Model               string
	Priority            int
	IsDefault           bool
	Tags                []string
	WhenJSON            json.RawMessage
	Multiplier          float64
	CostPer1kInput      *float64
	CostPer1kOutput     *float64
	CostPer1kCacheWrite *float64
	CostPer1kCacheRead  *float64
}

type UpdateLlmRouteParams struct {
	OrgID               uuid.UUID
	RouteID             uuid.UUID
	Model               string
	Priority            int
	IsDefault           bool
	Tags                []string
	WhenJSON            json.RawMessage
	Multiplier          float64
	CostPer1kInput      *float64
	CostPer1kOutput     *float64
	CostPer1kCacheWrite *float64
	CostPer1kCacheRead  *float64
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
func (r *LlmRoutesRepository) Create(ctx context.Context, params CreateLlmRouteParams) (LlmRoute, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if params.OrgID == uuid.Nil {
		return LlmRoute{}, fmt.Errorf("org_id must not be nil")
	}
	if params.CredentialID == uuid.Nil {
		return LlmRoute{}, fmt.Errorf("credential_id must not be nil")
	}
	params.Model = strings.TrimSpace(params.Model)
	if params.Model == "" {
		return LlmRoute{}, fmt.Errorf("model must not be empty")
	}
	params.Tags = normalizeRouteTags(params.Tags)
	if len(params.WhenJSON) == 0 {
		params.WhenJSON = json.RawMessage("{}")
	}
	if params.Multiplier <= 0 {
		params.Multiplier = 1.0
	}

	var route LlmRoute
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO llm_routes (org_id, credential_id, model, priority, is_default, tags, when_json, multiplier, cost_per_1k_input, cost_per_1k_output, cost_per_1k_cache_write, cost_per_1k_cache_read)
		 VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10, $11, $12)
		 RETURNING id, org_id, credential_id, model, priority, is_default, tags, when_json, multiplier, cost_per_1k_input, cost_per_1k_output, cost_per_1k_cache_write, cost_per_1k_cache_read, created_at`,
		params.OrgID, params.CredentialID, params.Model, params.Priority, params.IsDefault, params.Tags, string(params.WhenJSON), params.Multiplier,
		params.CostPer1kInput, params.CostPer1kOutput, params.CostPer1kCacheWrite, params.CostPer1kCacheRead,
	).Scan(
		&route.ID, &route.OrgID, &route.CredentialID, &route.Model,
		&route.Priority, &route.IsDefault, &route.Tags, &route.WhenJSON,
		&route.Multiplier, &route.CostPer1kInput, &route.CostPer1kOutput,
		&route.CostPer1kCacheWrite, &route.CostPer1kCacheRead,
		&route.CreatedAt,
	)
	if err != nil {
		return LlmRoute{}, mapLlmRouteWriteError(err, params.CredentialID, params.Model)
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
		`SELECT id, org_id, credential_id, model, priority, is_default, tags, when_json, multiplier, cost_per_1k_input, cost_per_1k_output, cost_per_1k_cache_write, cost_per_1k_cache_read, created_at
		 FROM llm_routes
		 WHERE org_id = $1 AND credential_id = $2
		 ORDER BY priority DESC, created_at ASC`,
		orgID, credentialID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	routes := []LlmRoute{}
	for rows.Next() {
		route, err := scanLlmRoute(rows)
		if err != nil {
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
		`SELECT r.id, r.org_id, r.credential_id, r.model, r.priority, r.is_default, r.tags, r.when_json, r.multiplier, r.cost_per_1k_input, r.cost_per_1k_output, r.cost_per_1k_cache_write, r.cost_per_1k_cache_read, r.created_at
		 FROM llm_routes r
		 JOIN llm_credentials c ON c.id = r.credential_id
		 WHERE r.org_id = $1 AND c.revoked_at IS NULL
		 ORDER BY r.priority DESC, r.created_at ASC`,
		orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	routes := []LlmRoute{}
	for rows.Next() {
		route, err := scanLlmRoute(rows)
		if err != nil {
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
		`SELECT r.id, r.org_id, r.credential_id, r.model, r.priority, r.is_default, r.tags, r.when_json, r.multiplier, r.cost_per_1k_input, r.cost_per_1k_output, r.cost_per_1k_cache_write, r.cost_per_1k_cache_read, r.created_at
		 FROM llm_routes r
		 JOIN llm_credentials c ON c.id = r.credential_id
		 WHERE c.revoked_at IS NULL
		 ORDER BY r.priority DESC, r.created_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	routes := []LlmRoute{}
	for rows.Next() {
		route, err := scanLlmRoute(rows)
		if err != nil {
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

	route, err := scanLlmRoute(r.db.QueryRow(
		ctx,
		`SELECT id, org_id, credential_id, model, priority, is_default, tags, when_json, multiplier, cost_per_1k_input, cost_per_1k_output, cost_per_1k_cache_write, cost_per_1k_cache_read, created_at
		 FROM llm_routes
		 WHERE id = $1 AND org_id = $2`,
		id, orgID,
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &route, nil
}

// Update 更新路由的可变字段。
func (r *LlmRoutesRepository) Update(ctx context.Context, params UpdateLlmRouteParams) (LlmRoute, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	params.Model = strings.TrimSpace(params.Model)
	if params.Model == "" {
		return LlmRoute{}, fmt.Errorf("model must not be empty")
	}
	params.Tags = normalizeRouteTags(params.Tags)
	if len(params.WhenJSON) == 0 {
		params.WhenJSON = json.RawMessage("{}")
	}
	if params.Multiplier <= 0 {
		params.Multiplier = 1.0
	}

	route, err := scanLlmRoute(r.db.QueryRow(
		ctx,
		`UPDATE llm_routes
		 SET model = $3, priority = $4, is_default = $5, tags = $6, when_json = $7::jsonb,
		     multiplier = $8, cost_per_1k_input = $9, cost_per_1k_output = $10,
		     cost_per_1k_cache_write = $11, cost_per_1k_cache_read = $12
		 WHERE id = $1 AND org_id = $2
		 RETURNING id, org_id, credential_id, model, priority, is_default, tags, when_json, multiplier, cost_per_1k_input, cost_per_1k_output, cost_per_1k_cache_write, cost_per_1k_cache_read, created_at`,
		params.RouteID, params.OrgID, params.Model, params.Priority, params.IsDefault, params.Tags, string(params.WhenJSON), params.Multiplier,
		params.CostPer1kInput, params.CostPer1kOutput, params.CostPer1kCacheWrite, params.CostPer1kCacheRead,
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return LlmRoute{}, fmt.Errorf("route not found")
		}
		return LlmRoute{}, mapLlmRouteWriteError(err, uuid.Nil, params.Model)
	}
	return route, nil
}

// SetDefaultByCredential 将指定凭证的默认路由切换为 routeID。
func (r *LlmRoutesRepository) SetDefaultByCredential(ctx context.Context, orgID, credentialID, routeID uuid.UUID) (*LlmRoute, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if orgID == uuid.Nil || credentialID == uuid.Nil || routeID == uuid.Nil {
		return nil, fmt.Errorf("org_id, credential_id and route_id must not be nil")
	}

	route, err := scanLlmRoute(r.db.QueryRow(
		ctx,
		`WITH updated AS (
			UPDATE llm_routes
			SET is_default = (id = $3)
			WHERE org_id = $1 AND credential_id = $2
			RETURNING id, org_id, credential_id, model, priority, is_default, tags, when_json, multiplier, cost_per_1k_input, cost_per_1k_output, cost_per_1k_cache_write, cost_per_1k_cache_read, created_at
		)
		SELECT id, org_id, credential_id, model, priority, is_default, tags, when_json, multiplier, cost_per_1k_input, cost_per_1k_output, cost_per_1k_cache_write, cost_per_1k_cache_read, created_at
		FROM updated
		WHERE id = $3`,
		orgID, credentialID, routeID,
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &route, nil
}

// PromoteHighestPriorityToDefault 将优先级最高的路由提升为默认，找不到返回 nil。
func (r *LlmRoutesRepository) PromoteHighestPriorityToDefault(ctx context.Context, orgID, credentialID uuid.UUID) (*LlmRoute, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if orgID == uuid.Nil || credentialID == uuid.Nil {
		return nil, fmt.Errorf("org_id and credential_id must not be nil")
	}

	route, err := scanLlmRoute(r.db.QueryRow(
		ctx,
		`WITH candidate AS (
			SELECT id
			FROM llm_routes
			WHERE org_id = $1 AND credential_id = $2
			ORDER BY priority DESC, created_at ASC, id ASC
			LIMIT 1
		), updated AS (
			UPDATE llm_routes
			SET is_default = (id = (SELECT id FROM candidate))
			WHERE org_id = $1 AND credential_id = $2
			RETURNING id, org_id, credential_id, model, priority, is_default, tags, when_json, multiplier, cost_per_1k_input, cost_per_1k_output, cost_per_1k_cache_write, cost_per_1k_cache_read, created_at
		)
		SELECT id, org_id, credential_id, model, priority, is_default, tags, when_json, multiplier, cost_per_1k_input, cost_per_1k_output, cost_per_1k_cache_write, cost_per_1k_cache_read, created_at
		FROM updated
		WHERE id = (SELECT id FROM candidate)`,
		orgID, credentialID,
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &route, nil
}

// DeleteByID 删除单个路由。找不到时静默成功。
func (r *LlmRoutesRepository) DeleteByID(ctx context.Context, orgID, routeID uuid.UUID) error {
	if ctx == nil {
		ctx = context.Background()
	}

	_, err := r.db.Exec(
		ctx,
		`DELETE FROM llm_routes WHERE id = $1 AND org_id = $2`,
		routeID, orgID,
	)
	return err
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

type llmRouteScanner interface {
	Scan(dest ...any) error
}

func scanLlmRoute(row llmRouteScanner) (LlmRoute, error) {
	var route LlmRoute
	err := row.Scan(
		&route.ID, &route.OrgID, &route.CredentialID, &route.Model,
		&route.Priority, &route.IsDefault, &route.Tags, &route.WhenJSON,
		&route.Multiplier, &route.CostPer1kInput, &route.CostPer1kOutput,
		&route.CostPer1kCacheWrite, &route.CostPer1kCacheRead,
		&route.CreatedAt,
	)
	return route, err
}

func normalizeRouteTags(tags []string) []string {
	if len(tags) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(tags))
	normalized := make([]string, 0, len(tags))
	for _, tag := range tags {
		cleaned := strings.TrimSpace(tag)
		if cleaned == "" {
			continue
		}
		key := strings.ToLower(cleaned)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, cleaned)
	}
	return normalized
}

func mapLlmRouteWriteError(err error, credentialID uuid.UUID, model string) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	if pgErr.Code != "23505" {
		return err
	}
	if pgErr.ConstraintName == "ux_llm_routes_credential_model_lower" {
		return LlmRouteModelConflictError{CredentialID: credentialID, Model: model}
	}
	return err
}

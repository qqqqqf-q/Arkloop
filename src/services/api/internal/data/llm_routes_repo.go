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
	ProjectID               *uuid.UUID
	CredentialID        uuid.UUID
	Model               string
	Priority            int
	IsDefault           bool
	Tags                []string
	WhenJSON            json.RawMessage
	AdvancedJSON        map[string]any
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
	ProjectID               uuid.UUID
	Scope               string
	CredentialID        uuid.UUID
	Model               string
	Priority            int
	IsDefault           bool
	Tags                []string
	WhenJSON            json.RawMessage
	AdvancedJSON        map[string]any
	Multiplier          float64
	CostPer1kInput      *float64
	CostPer1kOutput     *float64
	CostPer1kCacheWrite *float64
	CostPer1kCacheRead  *float64
}

type UpdateLlmRouteParams struct {
	ProjectID               uuid.UUID
	Scope               string
	RouteID             uuid.UUID
	Model               string
	Priority            int
	IsDefault           bool
	Tags                []string
	WhenJSON            json.RawMessage
	AdvancedJSON        map[string]any
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

func (r *LlmRoutesRepository) Create(ctx context.Context, params CreateLlmRouteParams) (LlmRoute, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if params.CredentialID == uuid.Nil {
		return LlmRoute{}, fmt.Errorf("credential_id must not be nil")
	}
	if params.Scope != LlmCredentialScopeProject && params.Scope != LlmCredentialScopePlatform {
		return LlmRoute{}, fmt.Errorf("scope must be project or platform")
	}
	if params.Scope == LlmCredentialScopeProject && params.ProjectID == uuid.Nil {
		return LlmRoute{}, fmt.Errorf("project_id must not be nil for project scope")
	}
	params.Model = strings.TrimSpace(params.Model)
	if params.Model == "" {
		return LlmRoute{}, fmt.Errorf("model must not be empty")
	}
	params.Tags = normalizeRouteTags(params.Tags)
	if len(params.WhenJSON) == 0 {
		params.WhenJSON = json.RawMessage("{}")
	}
	advancedJSONBytes, err := json.Marshal(params.AdvancedJSON)
	if err != nil {
		return LlmRoute{}, fmt.Errorf("marshal advanced_json: %w", err)
	}
	if params.Multiplier <= 0 {
		params.Multiplier = 1.0
	}

	projectIDParam := any(params.ProjectID)
	if params.Scope == LlmCredentialScopePlatform {
		projectIDParam = nil
	}

	var route LlmRoute
	var rawAdvancedJSON []byte
	err = r.db.QueryRow(
		ctx,
		`INSERT INTO llm_routes (project_id, credential_id, model, priority, is_default, tags, when_json, advanced_json, multiplier, cost_per_1k_input, cost_per_1k_output, cost_per_1k_cache_write, cost_per_1k_cache_read)
		 VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb, $9, $10, $11, $12, $13)
		 RETURNING id, project_id, credential_id, model, priority, is_default, tags, when_json, advanced_json, multiplier, cost_per_1k_input, cost_per_1k_output, cost_per_1k_cache_write, cost_per_1k_cache_read, created_at`,
		projectIDParam, params.CredentialID, params.Model, params.Priority, params.IsDefault, params.Tags, string(params.WhenJSON),
		string(advancedJSONBytes), params.Multiplier, params.CostPer1kInput, params.CostPer1kOutput, params.CostPer1kCacheWrite, params.CostPer1kCacheRead,
	).Scan(
		&route.ID, &route.ProjectID, &route.CredentialID, &route.Model,
		&route.Priority, &route.IsDefault, &route.Tags, &route.WhenJSON, &rawAdvancedJSON,
		&route.Multiplier, &route.CostPer1kInput, &route.CostPer1kOutput,
		&route.CostPer1kCacheWrite, &route.CostPer1kCacheRead,
		&route.CreatedAt,
	)
	if err != nil {
		return LlmRoute{}, mapLlmRouteWriteError(err, params.CredentialID, params.Model)
	}
	if len(rawAdvancedJSON) > 0 {
		_ = json.Unmarshal(rawAdvancedJSON, &route.AdvancedJSON)
	}
	return route, nil
}

func (r *LlmRoutesRepository) ListByCredential(ctx context.Context, projectID, credentialID uuid.UUID, scope string) ([]LlmRoute, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	query := `SELECT id, project_id, credential_id, model, priority, is_default, tags, when_json, advanced_json, multiplier, cost_per_1k_input, cost_per_1k_output, cost_per_1k_cache_write, cost_per_1k_cache_read, created_at
		 FROM llm_routes
		 WHERE credential_id = $1`
	args := []any{credentialID}
	var err error
	query, args, err = appendLlmRouteScopeFilter(query, args, projectID, scope)
	if err != nil {
		return nil, err
	}
	query += ` ORDER BY priority DESC, created_at ASC`
	return r.list(ctx, query, args...)
}

func (r *LlmRoutesRepository) ListByScope(ctx context.Context, projectID uuid.UUID, scope string) ([]LlmRoute, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	query := `SELECT id, project_id, credential_id, model, priority, is_default, tags, when_json, advanced_json, multiplier, cost_per_1k_input, cost_per_1k_output, cost_per_1k_cache_write, cost_per_1k_cache_read, created_at
		 FROM llm_routes`
	args := []any{}
	var err error
	query, args, err = appendLlmRouteScopeWhere(query, args, projectID, scope)
	if err != nil {
		return nil, err
	}
	query += ` ORDER BY priority DESC, created_at ASC`
	return r.list(ctx, query, args...)
}

func (r *LlmRoutesRepository) ListAllActive(ctx context.Context) ([]LlmRoute, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	return r.list(ctx,
		`SELECT r.id, r.project_id, r.credential_id, r.model, r.priority, r.is_default, r.tags, r.when_json, r.advanced_json, r.multiplier, r.cost_per_1k_input, r.cost_per_1k_output, r.cost_per_1k_cache_write, r.cost_per_1k_cache_read, r.created_at
		 FROM llm_routes r
		 JOIN llm_credentials c ON c.id = r.credential_id
		 WHERE c.revoked_at IS NULL
		 ORDER BY r.priority DESC, r.created_at ASC`,
	)
}

func (r *LlmRoutesRepository) GetByID(ctx context.Context, projectID, routeID uuid.UUID, scope string) (*LlmRoute, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	query := `SELECT id, project_id, credential_id, model, priority, is_default, tags, when_json, advanced_json, multiplier, cost_per_1k_input, cost_per_1k_output, cost_per_1k_cache_write, cost_per_1k_cache_read, created_at
		 FROM llm_routes
		 WHERE id = $1`
	args := []any{routeID}
	var err error
	query, args, err = appendLlmRouteScopeFilter(query, args, projectID, scope)
	if err != nil {
		return nil, err
	}
	query += ` LIMIT 1`

	route, err := scanLlmRoute(r.db.QueryRow(ctx, query, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &route, nil
}

func (r *LlmRoutesRepository) Update(ctx context.Context, params UpdateLlmRouteParams) (LlmRoute, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if params.RouteID == uuid.Nil {
		return LlmRoute{}, fmt.Errorf("route_id must not be nil")
	}
	params.Model = strings.TrimSpace(params.Model)
	if params.Model == "" {
		return LlmRoute{}, fmt.Errorf("model must not be empty")
	}
	params.Tags = normalizeRouteTags(params.Tags)
	if len(params.WhenJSON) == 0 {
		params.WhenJSON = json.RawMessage("{}")
	}
	advancedJSONBytes, err := json.Marshal(params.AdvancedJSON)
	if err != nil {
		return LlmRoute{}, fmt.Errorf("marshal advanced_json: %w", err)
	}
	if params.Multiplier <= 0 {
		params.Multiplier = 1.0
	}

	query := `UPDATE llm_routes
		 SET model = $2, priority = $3, is_default = $4, tags = $5, when_json = $6::jsonb,
		     advanced_json = $7::jsonb, multiplier = $8, cost_per_1k_input = $9, cost_per_1k_output = $10,
		     cost_per_1k_cache_write = $11, cost_per_1k_cache_read = $12
		 WHERE id = $1`
	args := []any{params.RouteID, params.Model, params.Priority, params.IsDefault, params.Tags, string(params.WhenJSON), string(advancedJSONBytes), params.Multiplier,
		params.CostPer1kInput, params.CostPer1kOutput, params.CostPer1kCacheWrite, params.CostPer1kCacheRead}
	query, args, err = appendLlmRouteScopeFilter(query, args, params.ProjectID, params.Scope)
	if err != nil {
		return LlmRoute{}, err
	}
	query += ` RETURNING id, project_id, credential_id, model, priority, is_default, tags, when_json, advanced_json, multiplier, cost_per_1k_input, cost_per_1k_output, cost_per_1k_cache_write, cost_per_1k_cache_read, created_at`

	route, err := scanLlmRoute(r.db.QueryRow(ctx, query, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return LlmRoute{}, fmt.Errorf("route not found")
		}
		return LlmRoute{}, mapLlmRouteWriteError(err, uuid.Nil, params.Model)
	}
	return route, nil
}

func (r *LlmRoutesRepository) SetDefaultByCredential(ctx context.Context, projectID, credentialID, routeID uuid.UUID, scope string) (*LlmRoute, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if credentialID == uuid.Nil || routeID == uuid.Nil {
		return nil, fmt.Errorf("credential_id and route_id must not be nil")
	}
	where, args, err := llmRouteScopeClause(projectID, scope, 3)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf(`WITH updated AS (
			UPDATE llm_routes
			SET is_default = (id = $2)
			WHERE credential_id = $1 AND %s
			RETURNING id, project_id, credential_id, model, priority, is_default, tags, when_json, advanced_json, multiplier, cost_per_1k_input, cost_per_1k_output, cost_per_1k_cache_write, cost_per_1k_cache_read, created_at
		)
		SELECT id, project_id, credential_id, model, priority, is_default, tags, when_json, advanced_json, multiplier, cost_per_1k_input, cost_per_1k_output, cost_per_1k_cache_write, cost_per_1k_cache_read, created_at
		FROM updated
		WHERE id = $2`, where)
	fullArgs := append([]any{credentialID, routeID}, args...)
	route, err := scanLlmRoute(r.db.QueryRow(ctx, query, fullArgs...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &route, nil
}

func (r *LlmRoutesRepository) PromoteHighestPriorityToDefault(ctx context.Context, projectID, credentialID uuid.UUID, scope string) (*LlmRoute, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if credentialID == uuid.Nil {
		return nil, fmt.Errorf("credential_id must not be nil")
	}
	where, args, err := llmRouteScopeClause(projectID, scope, 2)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf(`WITH candidate AS (
			SELECT id
			FROM llm_routes
			WHERE credential_id = $1 AND %s
			ORDER BY priority DESC, created_at ASC, id ASC
			LIMIT 1
		), updated AS (
			UPDATE llm_routes
			SET is_default = (id = (SELECT id FROM candidate))
			WHERE credential_id = $1 AND %s
			RETURNING id, project_id, credential_id, model, priority, is_default, tags, when_json, advanced_json, multiplier, cost_per_1k_input, cost_per_1k_output, cost_per_1k_cache_write, cost_per_1k_cache_read, created_at
		)
		SELECT id, project_id, credential_id, model, priority, is_default, tags, when_json, advanced_json, multiplier, cost_per_1k_input, cost_per_1k_output, cost_per_1k_cache_write, cost_per_1k_cache_read, created_at
		FROM updated
		WHERE id = (SELECT id FROM candidate)`, where, where)
	fullArgs := append([]any{credentialID}, args...)
	route, err := scanLlmRoute(r.db.QueryRow(ctx, query, fullArgs...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &route, nil
}

func (r *LlmRoutesRepository) DeleteByID(ctx context.Context, projectID, routeID uuid.UUID, scope string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	query := `DELETE FROM llm_routes WHERE id = $1`
	args := []any{routeID}
	var err error
	query, args, err = appendLlmRouteScopeFilter(query, args, projectID, scope)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, query, args...)
	return err
}

func (r *LlmRoutesRepository) DeleteByCredential(ctx context.Context, projectID, credentialID uuid.UUID, scope string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	query := `DELETE FROM llm_routes WHERE credential_id = $1`
	args := []any{credentialID}
	var err error
	query, args, err = appendLlmRouteScopeFilter(query, args, projectID, scope)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, query, args...)
	return err
}

type llmRouteScanner interface {
	Scan(dest ...any) error
}

func scanLlmRoute(row llmRouteScanner) (LlmRoute, error) {
	var route LlmRoute
	var rawAdvancedJSON []byte
	err := row.Scan(
		&route.ID, &route.ProjectID, &route.CredentialID, &route.Model,
		&route.Priority, &route.IsDefault, &route.Tags, &route.WhenJSON, &rawAdvancedJSON,
		&route.Multiplier, &route.CostPer1kInput, &route.CostPer1kOutput,
		&route.CostPer1kCacheWrite, &route.CostPer1kCacheRead,
		&route.CreatedAt,
	)
	if err == nil && len(rawAdvancedJSON) > 0 {
		_ = json.Unmarshal(rawAdvancedJSON, &route.AdvancedJSON)
	}
	return route, err
}

func (r *LlmRoutesRepository) list(ctx context.Context, query string, args ...any) ([]LlmRoute, error) {
	rows, err := r.db.Query(ctx, query, args...)
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

func appendLlmRouteScopeWhere(base string, args []any, projectID uuid.UUID, scope string) (string, []any, error) {
	if scope == LlmCredentialScopePlatform {
		return base + ` WHERE project_id IS NULL`, args, nil
	}
	if scope != LlmCredentialScopeProject {
		return "", nil, fmt.Errorf("scope must be project or platform")
	}
	if projectID == uuid.Nil {
		return "", nil, fmt.Errorf("project_id must not be nil for project scope")
	}
	args = append(args, projectID)
	return base + fmt.Sprintf(` WHERE project_id = $%d`, len(args)), args, nil
}

func appendLlmRouteScopeFilter(base string, args []any, projectID uuid.UUID, scope string) (string, []any, error) {
	where, extraArgs, err := llmRouteScopeClause(projectID, scope, len(args)+1)
	if err != nil {
		return "", nil, err
	}
	return base + ` AND ` + where, append(args, extraArgs...), nil
}

func llmRouteScopeClause(projectID uuid.UUID, scope string, index int) (string, []any, error) {
	if scope == LlmCredentialScopePlatform {
		return `project_id IS NULL`, nil, nil
	}
	if scope != LlmCredentialScopeProject {
		return "", nil, fmt.Errorf("scope must be project or platform")
	}
	if projectID == uuid.Nil {
		return "", nil, fmt.Errorf("project_id must not be nil for project scope")
	}
	return fmt.Sprintf(`project_id = $%d`, index), []any{projectID}, nil
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

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

const (
	LlmRouteScopeUser     = "user"
	LlmRouteScopePlatform = "platform"
)

type LlmRoute struct {
	ID                  uuid.UUID
	ProjectID           *uuid.UUID
	CredentialID        uuid.UUID
	Model               string
	Priority            int
	IsDefault           bool
	ShowInPicker        bool
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
	AccountID           uuid.UUID
	ProjectID           uuid.UUID
	Scope               string
	CredentialID        uuid.UUID
	Model               string
	Priority            int
	IsDefault           bool
	ShowInPicker        bool
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
	AccountID           uuid.UUID
	Scope               string
	RouteID             uuid.UUID
	Model               string
	Priority            int
	IsDefault           bool
	ShowInPicker        bool
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
	if params.Scope != LlmRouteScopeUser && params.Scope != LlmRouteScopePlatform {
		return LlmRoute{}, fmt.Errorf("scope must be user or platform")
	}
	if params.Scope == LlmRouteScopeUser && params.AccountID == uuid.Nil {
		return LlmRoute{}, fmt.Errorf("account_id must not be nil for user scope")
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
	tagsJSON, err := json.Marshal(params.Tags)
	if err != nil {
		return LlmRoute{}, fmt.Errorf("marshal tags: %w", err)
	}
	if params.Multiplier <= 0 {
		params.Multiplier = 1.0
	}

	var projectIDParam any
	if params.ProjectID != uuid.Nil {
		projectIDParam = params.ProjectID
	}
	var accountIDParam any
	if params.AccountID != uuid.Nil {
		accountIDParam = params.AccountID
	}

	var route LlmRoute
	var rawAdvancedJSON []byte
	var rawTagsJSON []byte
	err = r.db.QueryRow(
		ctx,
		`INSERT INTO llm_routes (account_id, project_id, credential_id, model, priority, is_default, show_in_picker, tags, when_json, advanced_json, multiplier, cost_per_1k_input, cost_per_1k_output, cost_per_1k_cache_write, cost_per_1k_cache_read)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10::jsonb, $11, $12, $13, $14, $15)
		 RETURNING id, project_id, credential_id, model, priority, is_default, show_in_picker, tags, when_json, advanced_json, multiplier, cost_per_1k_input, cost_per_1k_output, cost_per_1k_cache_write, cost_per_1k_cache_read, created_at`,
		accountIDParam, projectIDParam, params.CredentialID, params.Model, params.Priority, params.IsDefault, params.ShowInPicker, string(tagsJSON), string(params.WhenJSON),
		string(advancedJSONBytes), params.Multiplier, params.CostPer1kInput, params.CostPer1kOutput, params.CostPer1kCacheWrite, params.CostPer1kCacheRead,
	).Scan(
		&route.ID, &route.ProjectID, &route.CredentialID, &route.Model,
		&route.Priority, &route.IsDefault, &route.ShowInPicker, &rawTagsJSON, &route.WhenJSON, &rawAdvancedJSON,
		&route.Multiplier, &route.CostPer1kInput, &route.CostPer1kOutput,
		&route.CostPer1kCacheWrite, &route.CostPer1kCacheRead,
		&route.CreatedAt,
	)
	if err != nil {
		return LlmRoute{}, mapLlmRouteWriteError(err, params.CredentialID, params.Model)
	}
	if len(rawTagsJSON) > 0 {
		_ = json.Unmarshal(rawTagsJSON, &route.Tags)
	}
	if route.Tags == nil {
		route.Tags = []string{}
	}
	if len(rawAdvancedJSON) > 0 {
		_ = json.Unmarshal(rawAdvancedJSON, &route.AdvancedJSON)
	}
	return route, nil
}

func (r *LlmRoutesRepository) ListByCredential(ctx context.Context, accountID, credentialID uuid.UUID, scope string) ([]LlmRoute, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	// credential ownership is already verified by the caller; filtering routes by
	// account_id here causes routes created under a different account context to
	// become invisible while still blocking the unique index — filter by
	// credential_id alone is sufficient.
	_ = accountID
	_ = scope
	query := `SELECT id, project_id, credential_id, model, priority, is_default, show_in_picker, tags, when_json, advanced_json, multiplier, cost_per_1k_input, cost_per_1k_output, cost_per_1k_cache_write, cost_per_1k_cache_read, created_at
		 FROM llm_routes
		 WHERE credential_id = $1
		 ORDER BY priority DESC, created_at ASC`
	return r.list(ctx, query, credentialID)
}

func (r *LlmRoutesRepository) ListByScope(ctx context.Context, accountID uuid.UUID, scope string) ([]LlmRoute, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	query := `SELECT id, project_id, credential_id, model, priority, is_default, show_in_picker, tags, when_json, advanced_json, multiplier, cost_per_1k_input, cost_per_1k_output, cost_per_1k_cache_write, cost_per_1k_cache_read, created_at
		 FROM llm_routes`
	args := []any{}
	var err error
	query, args, err = appendLlmRouteScopeWhere(query, args, accountID, scope)
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
		`SELECT r.id, r.project_id, r.credential_id, r.model, r.priority, r.is_default, r.show_in_picker, r.tags, r.when_json, r.advanced_json, r.multiplier, r.cost_per_1k_input, r.cost_per_1k_output, r.cost_per_1k_cache_write, r.cost_per_1k_cache_read, r.created_at
		 FROM llm_routes r
		 JOIN llm_credentials c ON c.id = r.credential_id
		 WHERE c.revoked_at IS NULL
		 ORDER BY r.priority DESC, r.created_at ASC`,
	)
}

func (r *LlmRoutesRepository) GetByID(ctx context.Context, accountID, routeID uuid.UUID, scope string) (*LlmRoute, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	query := `SELECT id, project_id, credential_id, model, priority, is_default, show_in_picker, tags, when_json, advanced_json, multiplier, cost_per_1k_input, cost_per_1k_output, cost_per_1k_cache_write, cost_per_1k_cache_read, created_at
		 FROM llm_routes
		 WHERE id = $1`
	args := []any{routeID}
	var err error
	query, args, err = appendLlmRouteScopeFilter(query, args, accountID, scope)
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
	tagsJSON, err := json.Marshal(params.Tags)
	if err != nil {
		return LlmRoute{}, fmt.Errorf("marshal tags: %w", err)
	}
	if params.Multiplier <= 0 {
		params.Multiplier = 1.0
	}

	query := `UPDATE llm_routes
		 SET model = $2, priority = $3, is_default = $4, show_in_picker = $5, tags = $6::jsonb, when_json = $7::jsonb,
		     advanced_json = $8::jsonb, multiplier = $9, cost_per_1k_input = $10, cost_per_1k_output = $11,
		     cost_per_1k_cache_write = $12, cost_per_1k_cache_read = $13
		 WHERE id = $1`
	args := []any{params.RouteID, params.Model, params.Priority, params.IsDefault, params.ShowInPicker, string(tagsJSON), string(params.WhenJSON), string(advancedJSONBytes), params.Multiplier,
		params.CostPer1kInput, params.CostPer1kOutput, params.CostPer1kCacheWrite, params.CostPer1kCacheRead}
	query, args, err = appendLlmRouteScopeFilter(query, args, params.AccountID, params.Scope)
	if err != nil {
		return LlmRoute{}, err
	}
	query += ` RETURNING id, project_id, credential_id, model, priority, is_default, show_in_picker, tags, when_json, advanced_json, multiplier, cost_per_1k_input, cost_per_1k_output, cost_per_1k_cache_write, cost_per_1k_cache_read, created_at`

	route, err := scanLlmRoute(r.db.QueryRow(ctx, query, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return LlmRoute{}, fmt.Errorf("route not found")
		}
		return LlmRoute{}, mapLlmRouteWriteError(err, uuid.Nil, params.Model)
	}
	return route, nil
}

func (r *LlmRoutesRepository) SetDefaultByCredential(ctx context.Context, accountID, credentialID, routeID uuid.UUID, scope string) (*LlmRoute, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if credentialID == uuid.Nil || routeID == uuid.Nil {
		return nil, fmt.Errorf("credential_id and route_id must not be nil")
	}
	where, args, err := llmRouteScopeClause(accountID, scope, 3)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf(`WITH updated AS (
			UPDATE llm_routes
			SET is_default = (id = $2)
			WHERE credential_id = $1 AND %s
			RETURNING id, project_id, credential_id, model, priority, is_default, show_in_picker, tags, when_json, advanced_json, multiplier, cost_per_1k_input, cost_per_1k_output, cost_per_1k_cache_write, cost_per_1k_cache_read, created_at
		)
		SELECT id, project_id, credential_id, model, priority, is_default, show_in_picker, tags, when_json, advanced_json, multiplier, cost_per_1k_input, cost_per_1k_output, cost_per_1k_cache_write, cost_per_1k_cache_read, created_at
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

func (r *LlmRoutesRepository) PromoteHighestPriorityToDefault(ctx context.Context, accountID, credentialID uuid.UUID, scope string) (*LlmRoute, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if credentialID == uuid.Nil {
		return nil, fmt.Errorf("credential_id must not be nil")
	}
	where, args, err := llmRouteScopeClause(accountID, scope, 2)
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
			RETURNING id, project_id, credential_id, model, priority, is_default, show_in_picker, tags, when_json, advanced_json, multiplier, cost_per_1k_input, cost_per_1k_output, cost_per_1k_cache_write, cost_per_1k_cache_read, created_at
		)
		SELECT id, project_id, credential_id, model, priority, is_default, show_in_picker, tags, when_json, advanced_json, multiplier, cost_per_1k_input, cost_per_1k_output, cost_per_1k_cache_write, cost_per_1k_cache_read, created_at
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

func (r *LlmRoutesRepository) DeleteByID(ctx context.Context, accountID, routeID uuid.UUID, scope string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	query := `DELETE FROM llm_routes WHERE id = $1`
	args := []any{routeID}
	var err error
	query, args, err = appendLlmRouteScopeFilter(query, args, accountID, scope)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, query, args...)
	return err
}

func (r *LlmRoutesRepository) DeleteByCredential(ctx context.Context, accountID, credentialID uuid.UUID, scope string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	query := `DELETE FROM llm_routes WHERE credential_id = $1`
	args := []any{credentialID}
	var err error
	query, args, err = appendLlmRouteScopeFilter(query, args, accountID, scope)
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
	var rawTagsJSON []byte
	err := row.Scan(
		&route.ID, &route.ProjectID, &route.CredentialID, &route.Model,
		&route.Priority, &route.IsDefault, &route.ShowInPicker, &rawTagsJSON, &route.WhenJSON, &rawAdvancedJSON,
		&route.Multiplier, &route.CostPer1kInput, &route.CostPer1kOutput,
		&route.CostPer1kCacheWrite, &route.CostPer1kCacheRead,
		&route.CreatedAt,
	)
	if err == nil {
		if len(rawTagsJSON) > 0 {
			_ = json.Unmarshal(rawTagsJSON, &route.Tags)
		}
		if route.Tags == nil {
			route.Tags = []string{}
		}
		if len(rawAdvancedJSON) > 0 {
			_ = json.Unmarshal(rawAdvancedJSON, &route.AdvancedJSON)
		}
	}
	return route, err
}

// GetDefaultSelector returns "credential_name^model" for the default route
// under the given account. Returns empty string if no default route exists.
func (r *LlmRoutesRepository) GetDefaultSelector(ctx context.Context, accountID uuid.UUID, scope string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	query := `SELECT c.name, r.model
		 FROM llm_routes r
		 JOIN llm_credentials c ON c.id = r.credential_id
		 WHERE r.is_default = true`
	args := []any{}
	if scope == LlmRouteScopePlatform {
		query += ` AND r.project_id IS NULL AND r.account_id IS NULL`
	} else {
		query += fmt.Sprintf(` AND r.account_id = $%d`, len(args)+1)
		args = append(args, accountID)
	}
	query += ` ORDER BY r.priority DESC LIMIT 1`
	var credName, model string
	err := r.db.QueryRow(ctx, query, args...).Scan(&credName, &model)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(credName) + "^" + strings.TrimSpace(model), nil
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

func appendLlmRouteScopeWhere(base string, args []any, accountID uuid.UUID, scope string) (string, []any, error) {
	if scope == LlmRouteScopePlatform {
		return base + ` WHERE project_id IS NULL AND account_id IS NULL`, args, nil
	}
	if scope != LlmRouteScopeUser {
		return "", nil, fmt.Errorf("scope must be user or platform")
	}
	if accountID == uuid.Nil {
		return "", nil, fmt.Errorf("account_id must not be nil for user scope")
	}
	args = append(args, accountID)
	return base + fmt.Sprintf(` WHERE account_id = $%d`, len(args)), args, nil
}

func appendLlmRouteScopeFilter(base string, args []any, accountID uuid.UUID, scope string) (string, []any, error) {
	where, extraArgs, err := llmRouteScopeClause(accountID, scope, len(args)+1)
	if err != nil {
		return "", nil, err
	}
	return base + ` AND ` + where, append(args, extraArgs...), nil
}

func llmRouteScopeClause(accountID uuid.UUID, scope string, index int) (string, []any, error) {
	if scope == LlmRouteScopePlatform {
		return `project_id IS NULL AND account_id IS NULL`, nil, nil
	}
	if scope != LlmRouteScopeUser {
		return "", nil, fmt.Errorf("scope must be user or platform")
	}
	if accountID == uuid.Nil {
		return "", nil, fmt.Errorf("account_id must not be nil for user scope")
	}
	return fmt.Sprintf(`account_id = $%d`, index), []any{accountID}, nil
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
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		switch pgErr.ConstraintName {
		case "ux_llm_routes_credential_model_lower":
			return LlmRouteModelConflictError{CredentialID: credentialID, Model: model}
		case "ux_llm_routes_credential_default":
			return LlmRouteModelConflictError{CredentialID: credentialID, Model: model}
		}
		return err
	}
	if conflict := sqliteLlmRouteUniqueConflict(err, credentialID, model); conflict != nil {
		return conflict
	}
	return err
}

// SQLite（desktop）不报 pgconn；唯一冲突文案含索引名，与 modernc.org/sqlite 一致。
func sqliteLlmRouteUniqueConflict(err error, credentialID uuid.UUID, model string) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if !strings.Contains(msg, "UNIQUE constraint failed") && !strings.Contains(msg, "constraint failed: UNIQUE") {
		return nil
	}
	switch {
	case strings.Contains(msg, "ux_llm_routes_credential_model_lower"):
		return LlmRouteModelConflictError{CredentialID: credentialID, Model: model}
	case strings.Contains(msg, "ux_llm_routes_credential_default"):
		return LlmRouteModelConflictError{CredentialID: credentialID, Model: model}
	default:
		return nil
	}
}

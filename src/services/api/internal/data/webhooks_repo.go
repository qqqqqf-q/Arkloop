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

type WebhookEndpoint struct {
	ID        uuid.UUID
	OrgID     uuid.UUID
	URL       string
	SecretID  *uuid.UUID
	Events    []string
	Enabled   bool
	CreatedAt time.Time
}

type WebhookEndpointRepository struct {
	db Querier
}

func NewWebhookEndpointRepository(db Querier) (*WebhookEndpointRepository, error) {
	if db == nil {
		return nil, errors.New("db must not be nil")
	}
	return &WebhookEndpointRepository{db: db}, nil
}

func (r *WebhookEndpointRepository) WithTx(tx pgx.Tx) *WebhookEndpointRepository {
	return &WebhookEndpointRepository{db: tx}
}

func (r *WebhookEndpointRepository) Create(ctx context.Context, id uuid.UUID, orgID uuid.UUID, url string, secretID uuid.UUID, events []string) (WebhookEndpoint, error) {
	if orgID == uuid.Nil {
		return WebhookEndpoint{}, fmt.Errorf("webhooks: org_id must not be empty")
	}
	if strings.TrimSpace(url) == "" {
		return WebhookEndpoint{}, fmt.Errorf("webhooks: url must not be empty")
	}
	if secretID == uuid.Nil {
		return WebhookEndpoint{}, fmt.Errorf("webhooks: secret_id must not be empty")
	}
	if len(events) == 0 {
		return WebhookEndpoint{}, fmt.Errorf("webhooks: events must not be empty")
	}
	if id == uuid.Nil {
		id = uuid.New()
	}

	var ep WebhookEndpoint
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO webhook_endpoints (id, org_id, url, secret_id, events)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, org_id, url, secret_id, events, enabled, created_at`,
		id, orgID, url, secretID, events,
	).Scan(
		&ep.ID, &ep.OrgID, &ep.URL, &ep.SecretID,
		&ep.Events, &ep.Enabled, &ep.CreatedAt,
	)
	if err != nil {
		return WebhookEndpoint{}, fmt.Errorf("webhooks.Create: %w", err)
	}
	return ep, nil
}

func (r *WebhookEndpointRepository) GetByID(ctx context.Context, id uuid.UUID) (*WebhookEndpoint, error) {
	var ep WebhookEndpoint
	err := r.db.QueryRow(
		ctx,
		`SELECT id, org_id, url, secret_id, events, enabled, created_at
		 FROM webhook_endpoints WHERE id = $1`,
		id,
	).Scan(
		&ep.ID, &ep.OrgID, &ep.URL, &ep.SecretID,
		&ep.Events, &ep.Enabled, &ep.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("webhooks.GetByID: %w", err)
	}
	return &ep, nil
}

func (r *WebhookEndpointRepository) ListByOrg(ctx context.Context, orgID uuid.UUID) ([]WebhookEndpoint, error) {
	rows, err := r.db.Query(
		ctx,
		`SELECT id, org_id, url, secret_id, events, enabled, created_at
		 FROM webhook_endpoints
		 WHERE org_id = $1
		 ORDER BY created_at ASC`,
		orgID,
	)
	if err != nil {
		return nil, fmt.Errorf("webhooks.ListByOrg: %w", err)
	}
	defer rows.Close()

	var endpoints []WebhookEndpoint
	for rows.Next() {
		var ep WebhookEndpoint
		if err := rows.Scan(
			&ep.ID, &ep.OrgID, &ep.URL, &ep.SecretID,
			&ep.Events, &ep.Enabled, &ep.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("webhooks.ListByOrg scan: %w", err)
		}
		endpoints = append(endpoints, ep)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("webhooks.ListByOrg rows: %w", err)
	}
	return endpoints, nil
}

func (r *WebhookEndpointRepository) SetEnabled(ctx context.Context, id uuid.UUID, enabled bool) (*WebhookEndpoint, error) {
	var ep WebhookEndpoint
	err := r.db.QueryRow(
		ctx,
		`UPDATE webhook_endpoints SET enabled = $2
		 WHERE id = $1
		 RETURNING id, org_id, url, secret_id, events, enabled, created_at`,
		id, enabled,
	).Scan(
		&ep.ID, &ep.OrgID, &ep.URL, &ep.SecretID,
		&ep.Events, &ep.Enabled, &ep.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("webhooks.SetEnabled: %w", err)
	}
	return &ep, nil
}

// Delete 删除指定 org 下的 webhook 端点，通过 org_id 条件避免越权删除。
func (r *WebhookEndpointRepository) Delete(ctx context.Context, id uuid.UUID, orgID uuid.UUID) error {
	tag, err := r.db.Exec(
		ctx,
		`DELETE FROM webhook_endpoints WHERE id = $1 AND org_id = $2`,
		id, orgID,
	)
	if err != nil {
		return fmt.Errorf("webhooks.Delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	return nil
}

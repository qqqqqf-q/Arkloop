package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type WebhookEndpoint struct {
	ID            uuid.UUID
	OrgID         uuid.UUID
	URL           string
	SigningSecret string
	Events        []string
	Enabled       bool
	CreatedAt     time.Time
}

type WebhookDelivery struct {
	ID             uuid.UUID
	EndpointID     uuid.UUID
	OrgID          uuid.UUID
	EventType      string
	PayloadJSON    []byte
	Status         string
	Attempts       int
	LastAttemptAt  *time.Time
	ResponseStatus *int
	ResponseBody   *string
	CreatedAt      time.Time
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

func (r *WebhookEndpointRepository) Create(
	ctx context.Context,
	orgID uuid.UUID,
	url string,
	signingSecret string,
	events []string,
) (WebhookEndpoint, error) {
	if orgID == uuid.Nil {
		return WebhookEndpoint{}, fmt.Errorf("org_id must not be empty")
	}
	if url == "" {
		return WebhookEndpoint{}, fmt.Errorf("url must not be empty")
	}
	if signingSecret == "" {
		return WebhookEndpoint{}, fmt.Errorf("signing_secret must not be empty")
	}
	if len(events) == 0 {
		return WebhookEndpoint{}, fmt.Errorf("events must not be empty")
	}

	var ep WebhookEndpoint
	err := r.db.QueryRow(
		ctx,
		`INSERT INTO webhook_endpoints (org_id, url, signing_secret, events)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, org_id, url, signing_secret, events, enabled, created_at`,
		orgID, url, signingSecret, events,
	).Scan(
		&ep.ID, &ep.OrgID, &ep.URL, &ep.SigningSecret,
		&ep.Events, &ep.Enabled, &ep.CreatedAt,
	)
	if err != nil {
		return WebhookEndpoint{}, err
	}
	return ep, nil
}

func (r *WebhookEndpointRepository) GetByID(ctx context.Context, id uuid.UUID) (*WebhookEndpoint, error) {
	var ep WebhookEndpoint
	err := r.db.QueryRow(
		ctx,
		`SELECT id, org_id, url, signing_secret, events, enabled, created_at
		 FROM webhook_endpoints WHERE id = $1`,
		id,
	).Scan(
		&ep.ID, &ep.OrgID, &ep.URL, &ep.SigningSecret,
		&ep.Events, &ep.Enabled, &ep.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ep, nil
}

func (r *WebhookEndpointRepository) ListByOrg(ctx context.Context, orgID uuid.UUID) ([]WebhookEndpoint, error) {
	rows, err := r.db.Query(
		ctx,
		`SELECT id, org_id, url, signing_secret, events, enabled, created_at
		 FROM webhook_endpoints
		 WHERE org_id = $1
		 ORDER BY created_at ASC`,
		orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var endpoints []WebhookEndpoint
	for rows.Next() {
		var ep WebhookEndpoint
		if err := rows.Scan(
			&ep.ID, &ep.OrgID, &ep.URL, &ep.SigningSecret,
			&ep.Events, &ep.Enabled, &ep.CreatedAt,
		); err != nil {
			return nil, err
		}
		endpoints = append(endpoints, ep)
	}
	return endpoints, rows.Err()
}

func (r *WebhookEndpointRepository) SetEnabled(ctx context.Context, id uuid.UUID, enabled bool) (*WebhookEndpoint, error) {
	var ep WebhookEndpoint
	err := r.db.QueryRow(
		ctx,
		`UPDATE webhook_endpoints SET enabled = $2
		 WHERE id = $1
		 RETURNING id, org_id, url, signing_secret, events, enabled, created_at`,
		id, enabled,
	).Scan(
		&ep.ID, &ep.OrgID, &ep.URL, &ep.SigningSecret,
		&ep.Events, &ep.Enabled, &ep.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ep, nil
}

func (r *WebhookEndpointRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(
		ctx,
		`DELETE FROM webhook_endpoints WHERE id = $1`,
		id,
	)
	return err
}

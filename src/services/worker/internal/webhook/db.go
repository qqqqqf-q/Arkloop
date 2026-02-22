package webhook

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type endpointRow struct {
	ID            uuid.UUID
	URL           string
	SigningSecret string
	Events        []string
	Enabled       bool
}

func getWebhookEndpoint(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (*endpointRow, error) {
	var ep endpointRow
	err := pool.QueryRow(ctx,
		`SELECT id, url, signing_secret, events, enabled
		 FROM webhook_endpoints WHERE id = $1`,
		id,
	).Scan(&ep.ID, &ep.URL, &ep.SigningSecret, &ep.Events, &ep.Enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !ep.Enabled {
		return nil, nil
	}
	return &ep, nil
}

// listEndpointsForEvent 返回指定 org 中订阅了给定事件类型的所有启用端点。
func listEndpointsForEvent(ctx context.Context, pool *pgxpool.Pool, orgID uuid.UUID, eventType string) ([]endpointRow, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, url, signing_secret, events, enabled
		 FROM webhook_endpoints
		 WHERE org_id = $1
		   AND enabled = true
		   AND $2 = ANY(events)`,
		orgID, eventType,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var endpoints []endpointRow
	for rows.Next() {
		var ep endpointRow
		if err := rows.Scan(&ep.ID, &ep.URL, &ep.SigningSecret, &ep.Events, &ep.Enabled); err != nil {
			return nil, err
		}
		endpoints = append(endpoints, ep)
	}
	return endpoints, rows.Err()
}

// insertDelivery 创建一条 pending 的投递记录，返回 delivery_id。
func insertDelivery(
	ctx context.Context,
	pool *pgxpool.Pool,
	endpointID uuid.UUID,
	orgID uuid.UUID,
	eventType string,
	payloadJSON []byte,
) (uuid.UUID, error) {
	var id uuid.UUID
	err := pool.QueryRow(ctx,
		`INSERT INTO webhook_deliveries (endpoint_id, org_id, event_type, payload_json)
		 VALUES ($1, $2, $3, $4::jsonb)
		 RETURNING id`,
		endpointID, orgID, eventType, string(payloadJSON),
	).Scan(&id)
	return id, err
}

func updateDeliveryAttempt(
	ctx context.Context,
	pool *pgxpool.Pool,
	deliveryID uuid.UUID,
	attempts int,
	responseStatus *int,
	responseBody string,
) error {
	now := time.Now()
	_, err := pool.Exec(ctx,
		`UPDATE webhook_deliveries
		 SET attempts = $2, last_attempt_at = $3,
		     response_status = $4, response_body = $5
		 WHERE id = $1`,
		deliveryID, attempts, now, responseStatus, responseBody,
	)
	return err
}

func markDeliveryDelivered(
	ctx context.Context,
	pool *pgxpool.Pool,
	deliveryID uuid.UUID,
	attempts int,
	responseStatus int,
	responseBody string,
) error {
	now := time.Now()
	_, err := pool.Exec(ctx,
		`UPDATE webhook_deliveries
		 SET status = 'delivered', attempts = $2, last_attempt_at = $3,
		     response_status = $4, response_body = $5
		 WHERE id = $1`,
		deliveryID, attempts, now, responseStatus, responseBody,
	)
	return err
}

func markDeliveryFailed(
	ctx context.Context,
	pool *pgxpool.Pool,
	deliveryID uuid.UUID,
	attempts int,
	responseStatus *int,
	responseBody *string,
) error {
	now := time.Now()
	_, err := pool.Exec(ctx,
		`UPDATE webhook_deliveries
		 SET status = 'failed', attempts = $2, last_attempt_at = $3,
		     response_status = $4, response_body = $5
		 WHERE id = $1`,
		deliveryID, attempts, now, responseStatus, responseBody,
	)
	return err
}

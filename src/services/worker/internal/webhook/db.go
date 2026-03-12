package webhook

import (
	"context"
	"errors"
	"time"

	workercrypto "arkloop/services/worker/internal/crypto"

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

// getWebhookEndpoint 查询端点，返回 (nil, true, nil) 表示端点存在但已禁用，(nil, false, nil) 表示不存在。
func getWebhookEndpoint(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (*endpointRow, bool, error) {
	var ep endpointRow
	var encryptedValue *string
	err := pool.QueryRow(ctx,
		`SELECT e.id, e.url, s.encrypted_value, e.events, e.enabled
		 FROM webhook_endpoints e
		 LEFT JOIN secrets s ON s.id = e.secret_id
		 WHERE e.id = $1`,
		id,
	).Scan(&ep.ID, &ep.URL, &encryptedValue, &ep.Events, &ep.Enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !ep.Enabled {
		return nil, true, nil
	}
	if err := hydrateWebhookSigningSecret(&ep, encryptedValue); err != nil {
		return nil, false, err
	}
	return &ep, false, nil
}

// listEndpointsForEvent 返回指定 org 中订阅了给定事件类型的所有启用端点。
func listEndpointsForEvent(ctx context.Context, pool *pgxpool.Pool, accountID uuid.UUID, eventType string) ([]endpointRow, error) {
	rows, err := pool.Query(ctx,
		`SELECT e.id, e.url, s.encrypted_value, e.events, e.enabled
		 FROM webhook_endpoints e
		 LEFT JOIN secrets s ON s.id = e.secret_id
		 WHERE e.account_id = $1
		   AND enabled = true
		   AND $2 = ANY(events)`,
		accountID, eventType,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var endpoints []endpointRow
	for rows.Next() {
		var ep endpointRow
		var encryptedValue *string
		if err := rows.Scan(&ep.ID, &ep.URL, &encryptedValue, &ep.Events, &ep.Enabled); err != nil {
			return nil, err
		}
		if err := hydrateWebhookSigningSecret(&ep, encryptedValue); err != nil {
			return nil, err
		}
		endpoints = append(endpoints, ep)
	}
	return endpoints, rows.Err()
}

func hydrateWebhookSigningSecret(ep *endpointRow, encryptedValue *string) error {
	if ep == nil {
		return nil
	}
	if encryptedValue != nil && *encryptedValue != "" {
		plaintext, err := workercrypto.DecryptGCM(*encryptedValue)
		if err != nil {
			return err
		}
		ep.SigningSecret = string(plaintext)
		return nil
	}
	return nil
}

// insertDelivery 创建一条 pending 的投递记录，返回 delivery_id。
func insertDelivery(
	ctx context.Context,
	pool *pgxpool.Pool,
	endpointID uuid.UUID,
	accountID uuid.UUID,
	eventType string,
	payloadJSON []byte,
) (uuid.UUID, error) {
	var id uuid.UUID
	err := pool.QueryRow(ctx,
		`INSERT INTO webhook_deliveries (endpoint_id, account_id, event_type, payload_json)
		 VALUES ($1, $2, $3, $4::jsonb)
		 RETURNING id`,
		endpointID, accountID, eventType, string(payloadJSON),
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

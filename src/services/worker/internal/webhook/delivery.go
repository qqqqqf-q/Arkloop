package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"arkloop/services/worker/internal/app"
	"arkloop/services/worker/internal/queue"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	DeliverJobType = queue.WebhookDeliverJobType

	maxDeliveryAttempts = 5
	deliveryTimeoutSec  = 10
	baseRetryDelaySec   = 15
)

// DeliveryHandler 处理 webhook.deliver 类型的 job。
type DeliveryHandler struct {
	pool      *pgxpool.Pool
	queue     queue.JobQueue
	logger    *app.JSONLogger
	httpClient *http.Client
}

func NewDeliveryHandler(pool *pgxpool.Pool, q queue.JobQueue, logger *app.JSONLogger) (*DeliveryHandler, error) {
	if pool == nil {
		return nil, fmt.Errorf("pool must not be nil")
	}
	if q == nil {
		return nil, fmt.Errorf("queue must not be nil")
	}
	if logger == nil {
		logger = app.NewJSONLogger("webhook", nil)
	}
	return &DeliveryHandler{
		pool:  pool,
		queue: q,
		logger: logger,
		httpClient: &http.Client{
			Timeout: deliveryTimeoutSec * time.Second,
		},
	}, nil
}

func (h *DeliveryHandler) Handle(ctx context.Context, lease queue.JobLease) error {
	p, err := parseDeliveryPayload(lease.PayloadJSON)
	if err != nil {
		h.logger.Error("invalid webhook.deliver payload", app.LogFields{JobID: strPtr(lease.JobID.String())}, map[string]any{"error": err.Error()})
		// 格式错误不重试，直接 ack（返回 nil）
		return nil
	}

	fields := app.LogFields{
		JobID: strPtr(lease.JobID.String()),
		OrgID: strPtr(p.OrgID.String()),
	}

	// 查询端点配置
	ep, err := getWebhookEndpoint(ctx, h.pool, p.EndpointID)
	if err != nil {
		h.logger.Error("fetch webhook endpoint failed", fields, map[string]any{"error": err.Error()})
		return err
	}
	if ep == nil {
		// 端点已删除，跳过
		h.logger.Info("webhook endpoint deleted, skip", fields, nil)
		_ = markDeliveryFailed(ctx, h.pool, p.DeliveryID, lease.Attempts, nil, nil)
		return nil
	}

	// 构建签名
	payloadBytes, err := json.Marshal(p.Payload)
	if err != nil {
		return err
	}
	signature := computeHMAC(payloadBytes, ep.SigningSecret)

	// 发起 HTTP POST
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.URL, strings.NewReader(string(payloadBytes)))
	if err != nil {
		h.logger.Error("create http request failed", fields, map[string]any{"error": err.Error()})
		return h.handleFailure(ctx, p, lease, fields, nil, err.Error())
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Arkloop-Signature", "sha256="+signature)
	req.Header.Set("X-Arkloop-Event", p.EventType)
	req.Header.Set("X-Arkloop-Delivery", p.DeliveryID.String())

	resp, err := h.httpClient.Do(req)
	if err != nil {
		h.logger.Error("webhook http post failed", fields, map[string]any{"url": ep.URL, "error": err.Error()})
		return h.handleFailure(ctx, p, lease, fields, nil, err.Error())
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	bodyStr := string(bodyBytes)
	statusCode := resp.StatusCode

	if statusCode >= 200 && statusCode < 300 {
		_ = markDeliveryDelivered(ctx, h.pool, p.DeliveryID, lease.Attempts+1, statusCode, bodyStr)
		h.logger.Info("webhook delivered", fields, map[string]any{"url": ep.URL, "status": statusCode})
		return nil
	}

	h.logger.Info("webhook non-2xx response", fields, map[string]any{"url": ep.URL, "status": statusCode})
	return h.handleFailure(ctx, p, lease, fields, &statusCode, bodyStr)
}

// handleFailure 在投递失败时决定是重试还是最终失败。
func (h *DeliveryHandler) handleFailure(
	ctx context.Context,
	p deliveryPayload,
	lease queue.JobLease,
	fields app.LogFields,
	statusCode *int,
	responseBody string,
) error {
	attempts := lease.Attempts + 1
	_ = updateDeliveryAttempt(ctx, h.pool, p.DeliveryID, attempts, statusCode, responseBody)

	if attempts >= maxDeliveryAttempts {
		h.logger.Info("webhook max attempts reached, mark failed", fields, map[string]any{"attempts": attempts})
		_ = markDeliveryFailed(ctx, h.pool, p.DeliveryID, attempts, statusCode, &responseBody)
		return nil
	}

	// 指数退避重入队：delay = baseRetryDelaySec * 2^attempts
	delaySec := baseRetryDelaySec * (1 << attempts)
	availableAt := time.Now().Add(time.Duration(delaySec) * time.Second)

	newPayload := map[string]any{
		"endpoint_id": p.EndpointID.String(),
		"delivery_id": p.DeliveryID.String(),
		"event_type":  p.EventType,
		"payload":     p.Payload,
	}
	_, err := h.queue.EnqueueRun(
		ctx,
		p.OrgID,
		p.RunID,
		p.TraceID,
		DeliverJobType,
		newPayload,
		&availableAt,
	)
	if err != nil {
		h.logger.Error("re-enqueue webhook deliver failed", fields, map[string]any{"error": err.Error()})
	}
	return nil
}

// deliveryPayload 是 webhook.deliver job 的载荷结构。
type deliveryPayload struct {
	OrgID      uuid.UUID
	RunID      uuid.UUID
	TraceID    string
	EndpointID uuid.UUID
	DeliveryID uuid.UUID
	EventType  string
	Payload    map[string]any
}

func parseDeliveryPayload(raw map[string]any) (deliveryPayload, error) {
	orgID, err := requiredUUID(raw, "org_id")
	if err != nil {
		return deliveryPayload{}, err
	}
	runID, err := requiredUUID(raw, "run_id")
	if err != nil {
		return deliveryPayload{}, err
	}
	traceID, _ := raw["trace_id"].(string)

	inner, ok := raw["payload"].(map[string]any)
	if !ok {
		return deliveryPayload{}, fmt.Errorf("payload field missing or invalid")
	}

	endpointID, err := requiredUUID(inner, "endpoint_id")
	if err != nil {
		return deliveryPayload{}, err
	}
	deliveryID, err := requiredUUID(inner, "delivery_id")
	if err != nil {
		return deliveryPayload{}, err
	}
	eventType, ok := inner["event_type"].(string)
	if !ok || eventType == "" {
		return deliveryPayload{}, fmt.Errorf("event_type missing")
	}
	payload, _ := inner["payload"].(map[string]any)

	return deliveryPayload{
		OrgID:      orgID,
		RunID:      runID,
		TraceID:    traceID,
		EndpointID: endpointID,
		DeliveryID: deliveryID,
		EventType:  eventType,
		Payload:    payload,
	}, nil
}

func computeHMAC(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

func requiredUUID(values map[string]any, key string) (uuid.UUID, error) {
	raw, ok := values[key]
	if !ok {
		return uuid.Nil, fmt.Errorf("missing %s", key)
	}
	text, ok := raw.(string)
	if !ok || text == "" {
		return uuid.Nil, fmt.Errorf("%s must be a non-empty string", key)
	}
	id, err := uuid.Parse(text)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%s is not a valid UUID", key)
	}
	return id, nil
}

func strPtr(s string) *string {
	return &s
}

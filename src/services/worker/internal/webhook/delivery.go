package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
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
	maxRetryDelaySec    = 3600
)

// DeliveryHandler 处理 webhook.deliver 类型的 job。
type DeliveryHandler struct {
	pool       *pgxpool.Pool
	queue      queue.JobQueue
	logger     *app.JSONLogger
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
		pool:       pool,
		queue:      q,
		logger:     logger,
		httpClient: newSafeHTTPClient(),
	}, nil
}

// newSafeHTTPClient 创建阻断内网地址的 HTTP 客户端，防止 SSRF。
func newSafeHTTPClient() *http.Client {
	dialer := &net.Dialer{
		Timeout: deliveryTimeoutSec * time.Second,
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("webhook: invalid addr %q: %w", addr, err)
			}
			ips, err := net.DefaultResolver.LookupHost(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("webhook: dns lookup %q: %w", host, err)
			}
			for _, ipStr := range ips {
				ip := net.ParseIP(ipStr)
				if ip == nil {
					continue
				}
				if isPrivateIP(ip) {
					return nil, fmt.Errorf("webhook: target IP %s is not allowed (private/loopback/link-local)", ipStr)
				}
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(host, port))
		},
	}
	return &http.Client{
		Timeout:   deliveryTimeoutSec * time.Second,
		Transport: transport,
	}
}

// isPrivateIP 判断 IP 是否属于禁止访问的地址范围。
func isPrivateIP(ip net.IP) bool {
	privateRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"::1/128",
		"169.254.0.0/16", // link-local
		"fe80::/10",      // IPv6 link-local
		"fc00::/7",       // IPv6 unique local
		"100.64.0.0/10",  // RFC 6598 carrier-grade NAT
		"198.18.0.0/15",  // RFC 2544 benchmarking
	}
	for _, cidr := range privateRanges {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if network.Contains(ip) {
			return true
		}
	}
	return false
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
	ep, disabled, err := getWebhookEndpoint(ctx, h.pool, p.EndpointID)
	if err != nil {
		h.logger.Error("fetch webhook endpoint failed", fields, map[string]any{"error": err.Error()})
		return fmt.Errorf("get webhook endpoint: %w", err)
	}
	if ep == nil {
		if disabled {
			h.logger.Info("webhook endpoint disabled, skip", fields, nil)
		} else {
			h.logger.Info("webhook endpoint not found, skip", fields, nil)
			if err := markDeliveryFailed(ctx, h.pool, p.DeliveryID, lease.Attempts, nil, nil); err != nil {
				h.logger.Error("mark delivery failed error", fields, map[string]any{"error": err.Error()})
			}
		}
		return nil
	}

	// 构建带时间戳的签名（防重放）
	timestamp := time.Now().Unix()
	payloadBytes, err := json.Marshal(p.Payload)
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}
	signature := computeHMAC(timestamp, payloadBytes, ep.SigningSecret)

	// 发起 HTTP POST
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.URL, strings.NewReader(string(payloadBytes)))
	if err != nil {
		h.logger.Error("create http request failed", fields, map[string]any{"error": err.Error()})
		return h.handleFailure(ctx, p, lease, fields, nil, err.Error())
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Arkloop-Signature", "sha256="+signature)
	req.Header.Set("X-Arkloop-Timestamp", fmt.Sprintf("%d", timestamp))
	req.Header.Set("X-Arkloop-Event", p.EventType)
	req.Header.Set("X-Arkloop-Delivery", p.DeliveryID.String())

	resp, err := h.httpClient.Do(req)
	if err != nil {
		h.logger.Error("webhook http post failed", fields, map[string]any{"url": ep.URL, "error": err.Error()})
		return h.handleFailure(ctx, p, lease, fields, nil, err.Error())
	}
	defer resp.Body.Close()

	bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if readErr != nil {
		h.logger.Error("read webhook response body failed", fields, map[string]any{"error": readErr.Error()})
	}
	bodyStr := sanitizeWebhookResponseBody(bodyBytes)
	statusCode := resp.StatusCode

	if statusCode >= 200 && statusCode < 300 {
		if err := markDeliveryDelivered(ctx, h.pool, p.DeliveryID, lease.Attempts+1, statusCode, bodyStr); err != nil {
			h.logger.Error("mark delivery delivered failed", fields, map[string]any{"error": err.Error()})
		}
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
	if err := updateDeliveryAttempt(ctx, h.pool, p.DeliveryID, attempts, statusCode, responseBody); err != nil {
		h.logger.Error("update delivery attempt failed", fields, map[string]any{"error": err.Error()})
	}

	if attempts >= maxDeliveryAttempts {
		h.logger.Info("webhook max attempts reached, mark failed", fields, map[string]any{"attempts": attempts})
		if err := markDeliveryFailed(ctx, h.pool, p.DeliveryID, attempts, statusCode, &responseBody); err != nil {
			h.logger.Error("mark delivery failed error", fields, map[string]any{"error": err.Error()})
		}
		return nil
	}

	// 指数退避重入队：delay = baseRetryDelaySec * 2^attempts（有上界保护）
	delaySec := baseRetryDelaySec * (1 << attempts)
	if delaySec > maxRetryDelaySec || delaySec < 0 {
		delaySec = maxRetryDelaySec
	}
	availableAt := time.Now().Add(time.Duration(delaySec) * time.Second)

	newPayload := map[string]any{
		"endpoint_id": p.EndpointID.String(),
		"delivery_id": p.DeliveryID.String(),
		"event_type":  p.EventType,
		"payload":     p.Payload,
	}
	if _, err := h.queue.EnqueueRun(ctx, p.OrgID, p.RunID, p.TraceID, DeliverJobType, newPayload, &availableAt); err != nil {
		h.logger.Error("re-enqueue webhook deliver failed, marking delivery as failed", fields, map[string]any{"error": err.Error()})
		if markErr := markDeliveryFailed(ctx, h.pool, p.DeliveryID, attempts, statusCode, &responseBody); markErr != nil {
			h.logger.Error("mark delivery failed error", fields, map[string]any{"error": markErr.Error()})
		}
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

func sanitizeWebhookResponseBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	normalized := strings.ToValidUTF8(string(body), "�")
	return html.EscapeString(normalized)
}

// computeHMAC 计算带时间戳的 HMAC-SHA256 签名，格式为 HMAC(secret, "timestamp.payload")。
func computeHMAC(timestamp int64, payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.", timestamp)
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

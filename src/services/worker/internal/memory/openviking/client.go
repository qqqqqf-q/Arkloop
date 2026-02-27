package openviking

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"arkloop/services/worker/internal/memory"

	"github.com/google/uuid"
)

// retryBaseDelay 是重试的初始等待时间，每次翻倍。
const retryBaseDelay = 50 * time.Millisecond

const (
	defaultHTTPTimeout = 10 * time.Second
	maxReadRetries     = 2
)

// sanitizeAgentID 将 agentID 收敛到 OpenViking 要求的 [a-zA-Z0-9_-] 字符集。
var reInvalidAgentID = regexp.MustCompile(`[^a-zA-Z0-9_\-]`)

func sanitizeAgentID(id string) string {
	return reInvalidAgentID.ReplaceAllString(id, "_")
}

// agentSpace 计算 agent 隔离空间标识：sanitizedAgentID_<md5前8位>。
// 保留可读前缀便于线上排查，后缀保证租户隔离不依赖 agentID 全局唯一。
func agentSpace(userID uuid.UUID, agentID string) string {
	sum := md5.Sum([]byte(userID.String() + agentID))
	prefix := sanitizeAgentID(agentID)
	if len(prefix) > 16 {
		prefix = prefix[:16]
	}
	return fmt.Sprintf("%s_%x", prefix, sum[:4])
}

// scopeURI 将 MemoryScope 转换为对应的 viking URI 前缀，避免上层忘记拼 space 导致串租。
func scopeURI(scope memory.MemoryScope, ident memory.MemoryIdentity) string {
	switch scope {
	case memory.MemoryScopeAgent:
		return fmt.Sprintf("viking://agent/%s", agentSpace(ident.UserID, ident.AgentID))
	default:
		return fmt.Sprintf("viking://user/%s", ident.UserID.String())
	}
}

// setIdentityHeaders 附加 ROOT key 路线所需的多租户 header。
func setIdentityHeaders(req *http.Request, rootAPIKey string, ident memory.MemoryIdentity) {
	if rootAPIKey != "" {
		req.Header.Set("X-API-Key", rootAPIKey)
	}
	req.Header.Set("X-OpenViking-Account", ident.OrgID.String())
	req.Header.Set("X-OpenViking-User", ident.UserID.String())
	req.Header.Set("X-OpenViking-Agent", sanitizeAgentID(ident.AgentID))
}

// httpStatusError 携带 HTTP 状态码，供 doJSONWithRetry 判断是否可重试。
type httpStatusError struct {
	Status int
	Body   string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("status=%d body=%s", e.Status, e.Body)
}

// isRetryable 仅对网络错误和 5xx 重试；4xx 是客户端错误，重试无意义。
func isRetryable(err error) bool {
	var he *httpStatusError
	if errors.As(err, &he) {
		return he.Status >= 500
	}
	return true
}

// client 通过 OpenViking HTTP API 实现 MemoryProvider。
type client struct {
	baseURL    string
	rootAPIKey string
	http       *http.Client
}

func newClient(baseURL, rootAPIKey string) *client {
	return &client{
		baseURL:    baseURL,
		rootAPIKey: rootAPIKey,
		http:       &http.Client{Timeout: defaultHTTPTimeout},
	}
}

// doJSON 发起 JSON 请求并将响应 body 解码到 out。
func (c *client) doJSON(ctx context.Context, method, path string, body any, ident memory.MemoryIdentity, out any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	setIdentityHeaders(req, c.rootAPIKey, ident)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("openviking %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("openviking %s %s: %w", method, path,
			&httpStatusError{Status: resp.StatusCode, Body: string(raw)})
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// doJSONWithRetry 对幂等读接口提供带退避的重试（网络抖动 / 5xx）。
// 4xx 客户端错误不重试；ctx 取消时立即终止。
func (c *client) doJSONWithRetry(ctx context.Context, method, path string, body any, ident memory.MemoryIdentity, out any) error {
	var lastErr error
	for i := 0; i <= maxReadRetries; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := c.doJSON(ctx, method, path, body, ident, out); err != nil {
			lastErr = err
			if !isRetryable(err) {
				return err
			}
			if i < maxReadRetries {
				delay := retryBaseDelay * (1 << i)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(delay):
				}
			}
			continue
		}
		return nil
	}
	return lastErr
}

// --- Find ---

type findRequest struct {
	Query     string  `json:"query"`
	TargetURI string  `json:"target_uri,omitempty"`
	Limit     int     `json:"limit"`
	Threshold *float64 `json:"score_threshold,omitempty"`
}

// findResponse 对应 OpenViking /api/v1/search/find 返回的 result 字段。
type findResponse struct {
	Memories  []matchedContext `json:"memories"`
	Resources []matchedContext `json:"resources"`
	Skills    []matchedContext `json:"skills"`
}

type matchedContext struct {
	URI         string            `json:"uri"`
	Abstract    string            `json:"abstract"`
	Score       float64           `json:"score"`
	MatchReason string            `json:"match_reason"`
	Relations   []relatedContext  `json:"relations"`
}

type relatedContext struct {
	URI      string `json:"uri"`
	Abstract string `json:"abstract"`
}

type apiResponse struct {
	Status string          `json:"status"`
	Result json.RawMessage `json:"result"`
	Error  *apiError       `json:"error"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (c *client) Find(ctx context.Context, ident memory.MemoryIdentity, scope memory.MemoryScope, query string, limit int) ([]memory.MemoryHit, error) {
	req := findRequest{
		Query:     query,
		TargetURI: scopeURI(scope, ident),
		Limit:     limit,
	}

	var resp apiResponse
	if err := c.doJSONWithRetry(ctx, http.MethodPost, "/api/v1/search/find", req, ident, &resp); err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("openviking find error: [%s] %s", resp.Error.Code, resp.Error.Message)
	}

	var fr findResponse
	if err := json.Unmarshal(resp.Result, &fr); err != nil {
		return nil, fmt.Errorf("unmarshal find result: %w", err)
	}

	all := make([]matchedContext, 0, len(fr.Memories)+len(fr.Resources)+len(fr.Skills))
	all = append(all, fr.Memories...)
	all = append(all, fr.Resources...)
	all = append(all, fr.Skills...)

	hits := make([]memory.MemoryHit, 0, len(all))
	for _, mc := range all {
		hits = append(hits, memory.MemoryHit{
			URI:         mc.URI,
			Abstract:    mc.Abstract,
			Score:       mc.Score,
			MatchReason: mc.MatchReason,
			IsLeaf:      len(mc.Relations) == 0,
		})
	}
	return hits, nil
}

// --- Content ---

func (c *client) Content(ctx context.Context, ident memory.MemoryIdentity, uri string, layer memory.MemoryLayer) (string, error) {
	path := fmt.Sprintf("/api/v1/content/%s?uri=%s", string(layer), url.QueryEscape(uri))

	// GET 请求无 body，doJSONWithRetry 的 body 参数传 nil
	var resp apiResponse
	if err := c.doJSONWithRetry(ctx, http.MethodGet, path, nil, ident, &resp); err != nil {
		return "", err
	}
	if resp.Error != nil {
		return "", fmt.Errorf("openviking content error: [%s] %s", resp.Error.Code, resp.Error.Message)
	}

	// result 可能是字符串或对象，优先尝试 string
	var content string
	if err := json.Unmarshal(resp.Result, &content); err == nil {
		return content, nil
	}
	// fallback：将 result 原样返回为 JSON 字符串
	return string(resp.Result), nil
}

// --- AppendSessionMessages ---

type addMessageRequest struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (c *client) AppendSessionMessages(ctx context.Context, ident memory.MemoryIdentity, sessionID string, msgs []memory.MemoryMessage) error {
	// OpenViking sessions API 每次只接受一条消息。
	// 此函数是 best-effort：部分成功时已写入的消息不会回滚，调用方应在 goroutine 中
	// fire-and-forget，CommitSession 失败不影响 Run 主流程。
	for i, msg := range msgs {
		body := addMessageRequest{
			Role:    msg.Role,
			Content: msg.Content,
		}
		path := fmt.Sprintf("/api/v1/sessions/%s/messages", url.PathEscape(sessionID))
		if err := c.doJSON(ctx, http.MethodPost, path, body, ident, nil); err != nil {
			return fmt.Errorf("append message index=%d role=%s: %w", i, msg.Role, err)
		}
	}
	return nil
}

// --- CommitSession ---

func (c *client) CommitSession(ctx context.Context, ident memory.MemoryIdentity, sessionID string) error {
	path := fmt.Sprintf("/api/v1/sessions/%s/commit", url.PathEscape(sessionID))
	return c.doJSON(ctx, http.MethodPost, path, nil, ident, nil)
}

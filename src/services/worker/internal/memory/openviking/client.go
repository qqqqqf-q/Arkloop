package openviking

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	sharedoutbound "arkloop/services/shared/outboundurl"

	"arkloop/services/worker/internal/memory"
)

// retryBaseDelay 是重试的初始等待时间，每次翻倍。
const retryBaseDelay = 50 * time.Millisecond

const (
	defaultHTTPTimeout = 10 * time.Second
	// writeHTTPTimeout 用于 commit 等需要 LLM 处理的写操作，远长于普通 HTTP 调用。
	writeHTTPTimeout = 120 * time.Second
	maxReadRetries   = 2
)

// sanitizeAgentID 将 agentID 收敛到 OpenViking 要求的 [a-zA-Z0-9_-] 字符集。
var reInvalidAgentID = regexp.MustCompile(`[^a-zA-Z0-9_\-]`)

func sanitizeAgentID(id string) string {
	return reInvalidAgentID.ReplaceAllString(id, "_")
}

// setIdentityHeaders 附加 ROOT key 路线所需的多租户 header。
func setIdentityHeaders(req *http.Request, rootAPIKey string, ident memory.MemoryIdentity) {
	if rootAPIKey != "" {
		req.Header.Set("X-API-Key", rootAPIKey)
	}
	req.Header.Set("X-OpenViking-Account", ident.AccountID.String())
	userID := ident.UserID.String()
	if ident.ExternalUserID != "" {
		userID = ident.ExternalUserID
	}
	req.Header.Set("X-OpenViking-User", userID)
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
	http       *http.Client // 读操作（Find/Content）短超时
	writeHTTP  *http.Client // 写操作（commit/session）长超时，LLM 处理需要
	baseURLErr error
}

func newClient(baseURL, rootAPIKey string) *client {
	normalizedBaseURL, baseURLErr := sharedoutbound.DefaultPolicy().NormalizeInternalBaseURL(strings.TrimSpace(baseURL))
	if baseURLErr == nil {
		baseURL = normalizedBaseURL
	}
	return &client{
		baseURL:    baseURL,
		rootAPIKey: rootAPIKey,
		http:       sharedoutbound.DefaultPolicy().NewInternalHTTPClient(defaultHTTPTimeout),
		writeHTTP:  sharedoutbound.DefaultPolicy().NewInternalHTTPClient(writeHTTPTimeout),
		baseURLErr: baseURLErr,
	}
}

// doJSON 发起 JSON 请求并将响应 body 解码到 out。
func (c *client) doJSON(ctx context.Context, method, path string, body any, ident memory.MemoryIdentity, out any) error {
	return c.doJSONWith(ctx, c.http, method, path, body, ident, out)
}

// doWriteJSON 使用长超时 HTTP client，用于 commit 等需要 LLM 处理的写操作。
func (c *client) doWriteJSON(ctx context.Context, method, path string, body any, ident memory.MemoryIdentity, out any) error {
	return c.doJSONWith(ctx, c.writeHTTP, method, path, body, ident, out)
}

type contentWriteRequest struct {
	URI     string `json:"uri"`
	Content string `json:"content"`
	Mode    string `json:"mode"`
	Wait    bool   `json:"wait"`
}

func (c *client) doJSONWith(ctx context.Context, hc *http.Client, method, path string, body any, ident memory.MemoryIdentity, out any) error {
	if c.baseURLErr != nil {
		return c.baseURLErr
	}
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

	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("openviking %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

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
	Query     string   `json:"query"`
	TargetURI string   `json:"target_uri,omitempty"`
	Limit     int      `json:"limit"`
	Threshold *float64 `json:"score_threshold,omitempty"`
}

// findResponse 对应 OpenViking /api/v1/search/find 返回的 result 字段。
type findResponse struct {
	Memories  []matchedContext `json:"memories"`
	Resources []matchedContext `json:"resources"`
	Skills    []matchedContext `json:"skills"`
}

type matchedContext struct {
	URI         string   `json:"uri"`
	Abstract    string   `json:"abstract"`
	Score       float64  `json:"score"`
	MatchReason string   `json:"match_reason"`
	Relations   []string `json:"relations"`
}

// apiResponse 是 OpenViking 标准响应包装。
type apiResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *apiError       `json:"error,omitempty"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Find 实现 MemoryProvider.Find，经由 OpenViking /api/v1/search/find。
func (c *client) Find(ctx context.Context, ident memory.MemoryIdentity, targetURI string, query string, limit int) ([]memory.MemoryHit, error) {
	if strings.TrimSpace(query) == "" {
		return nil, errors.New("openviking find: query is empty")
	}
	if limit <= 0 {
		limit = 16
	}

	body := findRequest{
		Query:     query,
		TargetURI: strings.TrimSpace(targetURI),
		Limit:     limit,
	}
	if body.TargetURI == "" {
		body.TargetURI = fmt.Sprintf("viking://user/%s/memories/", ident.UserID.String())
	}

	var resp apiResponse
	if err := c.doJSONWithRetry(ctx, http.MethodPost, "/api/v1/search/find", body, ident, &resp); err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("openviking find error: [%s] %s", resp.Error.Code, resp.Error.Message)
	}

	var fr findResponse
	if err := json.Unmarshal(resp.Result, &fr); err != nil {
		return nil, fmt.Errorf("decode find result: %w", err)
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
	content, err := c.contentAtLayer(ctx, ident, uri, layer)
	if err == nil {
		return content, nil
	}
	if layer == memory.MemoryLayerOverview && shouldFallbackOverviewToRead(err) {
		return c.contentAtLayer(ctx, ident, uri, memory.MemoryLayerRead)
	}
	return "", err
}

func (c *client) contentAtLayer(ctx context.Context, ident memory.MemoryIdentity, uri string, layer memory.MemoryLayer) (string, error) {
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

func shouldFallbackOverviewToRead(err error) bool {
	var statusErr *httpStatusError
	if !errors.As(err, &statusErr) {
		return false
	}
	if statusErr.Status < 500 {
		return false
	}
	body := strings.ToLower(strings.TrimSpace(statusErr.Body))
	return strings.Contains(body, "is not a directory")
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
		if err := c.doWriteJSON(ctx, http.MethodPost, path, body, ident, nil); err != nil {
			return fmt.Errorf("append message index=%d role=%s: %w", i, msg.Role, err)
		}
	}
	return nil
}

// --- CommitSession ---

func (c *client) CommitSession(ctx context.Context, ident memory.MemoryIdentity, sessionID string) error {
	commitPath := fmt.Sprintf("/api/v1/sessions/%s/commit", url.PathEscape(sessionID))
	err := c.doWriteJSON(ctx, http.MethodPost, commitPath, nil, ident, nil)
	if err == nil {
		return nil
	}

	archiveID := c.extractFailedArchiveID(err)
	if archiveID == "" {
		return err
	}

	c.logFailedArchiveDetail(ctx, ident, sessionID, err)

	if delErr := c.deleteFailedArchive(ctx, ident, sessionID, archiveID); delErr != nil {
		slog.WarnContext(ctx, "memory: delete failed archive failed",
			"session_id", sessionID,
			"archive_id", archiveID,
			"err", delErr.Error(),
		)
		return err
	}

	slog.InfoContext(ctx, "memory: deleted failed archive, retrying commit",
		"session_id", sessionID,
		"archive_id", archiveID,
	)
	return c.doWriteJSON(ctx, http.MethodPost, commitPath, nil, ident, nil)
}

// logFailedArchiveDetail 当 commit 返回 412 时，尝试读取 .failed.json 以记录根因。
func (c *client) logFailedArchiveDetail(ctx context.Context, ident memory.MemoryIdentity, sessionID string, commitErr error) {
	archiveID := c.extractFailedArchiveID(commitErr)
	if archiveID == "" {
		return
	}

	userID := ident.UserID.String()
	if ident.ExternalUserID != "" {
		userID = ident.ExternalUserID
	}
	failedURI := fmt.Sprintf("viking://session/%s/%s/history/%s/.failed.json", userID, sessionID, archiveID)
	readPath := fmt.Sprintf("/api/v1/content/read?uri=%s", url.QueryEscape(failedURI))

	var resp apiResponse
	readErr := c.doJSON(ctx, http.MethodGet, readPath, nil, ident, &resp)
	if readErr != nil {
		slog.WarnContext(ctx, "memory: failed to read archive failure detail",
			"session_id", sessionID,
			"archive_id", archiveID,
			"read_err", readErr.Error(),
		)
		return
	}

	slog.WarnContext(ctx, "memory: archive failure detail",
		"session_id", sessionID,
		"archive_id", archiveID,
		"failed_json", string(resp.Result),
	)
}

// extractFailedArchiveID 从 412 响应中提取 archive_id；非 412 或解析失败时返回空字符串。
func (c *client) extractFailedArchiveID(err error) string {
	var he *httpStatusError
	if !errors.As(err, &he) || he.Status != http.StatusPreconditionFailed {
		return ""
	}
	var parsed struct {
		Error struct {
			Details struct {
				ArchiveID string `json:"archive_id"`
			} `json:"details"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(he.Body), &parsed) != nil {
		return ""
	}
	return parsed.Error.Details.ArchiveID
}

// deleteFailedArchive 通过 fs API 删除指定 session 下的 failed archive 目录。
func (c *client) deleteFailedArchive(ctx context.Context, ident memory.MemoryIdentity, sessionID, archiveID string) error {
	userID := ident.UserID.String()
	if ident.ExternalUserID != "" {
		userID = ident.ExternalUserID
	}
	archiveURI := fmt.Sprintf("viking://session/%s/%s/history/%s/", userID, sessionID, archiveID)
	path := "/api/v1/fs?uri=" + url.QueryEscape(archiveURI) + "&recursive=true"
	return c.doJSON(ctx, http.MethodDelete, path, nil, ident, nil)
}

// --- Write ---

// createSessionResult 对应 POST /api/v1/sessions 返回的 result 字段。
type createSessionResult struct {
	SessionID string `json:"session_id"`
}

// Write 通过"建立专属 session → 写入内容 → commit 触发提取"将结构化记忆写入指定 scope。
//
// OpenViking 的 commit 会在服务端异步处理 LLM 提取和向量化；
// 当前 Arkloop 的长期 memory 主体是 user，scope 仅用于内容组织，不改变 identity。
func (c *client) Write(ctx context.Context, ident memory.MemoryIdentity, scope memory.MemoryScope, entry memory.MemoryEntry) error {
	_ = scope
	if strings.TrimSpace(entry.Content) == "" {
		return errors.New("memory write: content is empty")
	}

	writeIdent := ident

	// 1. 创建临时 session
	var createResp apiResponse
	if err := c.doWriteJSON(ctx, http.MethodPost, "/api/v1/sessions", nil, writeIdent, &createResp); err != nil {
		return fmt.Errorf("memory write create session: %w", err)
	}
	if createResp.Error != nil {
		return fmt.Errorf("memory write create session: [%s] %s", createResp.Error.Code, createResp.Error.Message)
	}
	var sessionResult createSessionResult
	if err := json.Unmarshal(createResp.Result, &sessionResult); err != nil {
		return fmt.Errorf("memory write parse session id: %w", err)
	}
	sid := sessionResult.SessionID

	// 2. 写入内容作为 user 消息，assistant 回执确保 commit 不因空对话跳过提取
	msgPath := fmt.Sprintf("/api/v1/sessions/%s/messages", url.PathEscape(sid))
	for _, msg := range []addMessageRequest{
		{Role: "user", Content: entry.Content},
		{Role: "assistant", Content: "Noted."},
	} {
		if err := c.doWriteJSON(ctx, http.MethodPost, msgPath, msg, writeIdent, nil); err != nil {
			slog.WarnContext(ctx, "memory write: message failed, session leaked",
				"session_id", sid, "role", msg.Role, "err", err.Error())
			return fmt.Errorf("memory write add message role=%s: %w", msg.Role, err)
		}
	}

	// 3. commit 触发 LLM 提取（同步阻塞直到提取完成，用 writeHTTP 避免短超时截断）
	commitPath := fmt.Sprintf("/api/v1/sessions/%s/commit", url.PathEscape(sid))
	if err := c.doWriteJSON(ctx, http.MethodPost, commitPath, nil, writeIdent, nil); err != nil {
		slog.WarnContext(ctx, "memory write: commit failed, session leaked",
			"session_id", sid, "err", err.Error())
		return fmt.Errorf("memory write commit session=%s: %w", sid, err)
	}
	return nil
}

// --- Delete ---

// Delete 删除指定 URI 的记忆，同时从向量索引中移除（viking_fs.rm 保证两者一致）。
func (c *client) Delete(ctx context.Context, ident memory.MemoryIdentity, uri string) error {
	path := "/api/v1/fs?uri=" + url.QueryEscape(uri) + "&recursive=false"
	if err := c.doJSON(ctx, http.MethodDelete, path, nil, ident, nil); err != nil {
		return fmt.Errorf("memory delete uri=%s: %w", uri, err)
	}
	return nil
}

// --- ListDir ---

// ListDir lists direct children URIs under the given directory.
func (c *client) ListDir(ctx context.Context, ident memory.MemoryIdentity, uri string) ([]string, error) {
	path := "/api/v1/fs/ls?uri=" + url.QueryEscape(uri)
	var resp apiResponse
	if err := c.doJSONWithRetry(ctx, http.MethodGet, path, nil, ident, &resp); err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("openviking ls error: [%s] %s", resp.Error.Code, resp.Error.Message)
	}
	var rawEntries []struct {
		URI   string `json:"uri"`
		IsDir bool   `json:"isDir"`
	}
	if err := json.Unmarshal(resp.Result, &rawEntries); err != nil {
		return nil, fmt.Errorf("decode ls result: %w", err)
	}
	entries := make([]string, len(rawEntries))
	for i, e := range rawEntries {
		u := e.URI
		if e.IsDir && !strings.HasSuffix(u, "/") {
			u += "/"
		}
		entries[i] = u
	}
	return entries, nil
}

// UpdateByURI overwrites an existing semantic memory file in place and waits
// for semantic/vector refresh to complete.
func (c *client) UpdateByURI(ctx context.Context, ident memory.MemoryIdentity, uri string, entry memory.MemoryEntry) error {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return errors.New("memory edit: uri is empty")
	}
	if strings.TrimSpace(entry.Content) == "" {
		return errors.New("memory edit: content is empty")
	}
	body := contentWriteRequest{
		URI:     uri,
		Content: entry.Content,
		Mode:    "replace",
		Wait:    true,
	}
	if err := c.doWriteJSON(ctx, http.MethodPost, "/api/v1/content/write", body, ident, nil); err != nil {
		return fmt.Errorf("memory edit uri=%s: %w", uri, err)
	}
	return nil
}

package apiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	DefaultBaseURL  = "http://127.0.0.1:19001"
	DefaultToken    = ""
	ThreadPageLimit = 200
)

// Client 连接 Desktop API 的 HTTP 客户端。
// desktop token 只用于在本机换取 access token，不能直接访问 API。
type Client struct {
	baseURL      string
	desktopToken string
	httpClient   *http.Client // 用于普通 JSON 请求，10s 超时
	sseClient    *http.Client // 用于 SSE 流，header 超时 10s，body 无超时

	sessionMu   sync.Mutex
	accessToken string
}

// RunParams 创建 run 时的可选参数，零值字段不序列化。
type RunParams struct {
	PersonaID     string
	Model         string
	WorkDir       string
	ReasoningMode string
}

type Me struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	AccountID   string `json:"account_id"`
	WorkEnabled bool   `json:"work_enabled"`
}

type Persona struct {
	ID            string `json:"id"`
	PersonaKey    string `json:"persona_key"`
	DisplayName   string `json:"display_name"`
	SelectorName  string `json:"selector_name"`
	SelectorOrder int    `json:"selector_order"`
	Model         string `json:"model"`
	ReasoningMode string `json:"reasoning_mode"`
	Source        string `json:"source"`
}

type ProviderModel struct {
	ID           string   `json:"id"`
	ProviderID   string   `json:"provider_id"`
	Model        string   `json:"model"`
	IsDefault    bool     `json:"is_default"`
	ShowInPicker bool     `json:"show_in_picker"`
	Tags         []string `json:"tags"`
}

type LlmProvider struct {
	ID     string          `json:"id"`
	Name   string          `json:"name"`
	Models []ProviderModel `json:"models"`
}

type Thread struct {
	ID          string  `json:"id"`
	Mode        string  `json:"mode"`
	Title       *string `json:"title"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	ActiveRunID *string `json:"active_run_id"`
	IsPrivate   bool    `json:"is_private"`
}

type Run struct {
	RunID    string `json:"run_id"`
	ThreadID string `json:"thread_id"`
	Status   string `json:"status"`
}

// NewClient 构造客户端，baseURL 和 desktopToken 必须非空。
func NewClient(baseURL, desktopToken string) *Client {
	return &Client{
		baseURL:      baseURL,
		desktopToken: desktopToken,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		sseClient: &http.Client{
			Transport: &http.Transport{
				ResponseHeaderTimeout: 10 * time.Second,
			},
			// body 无整体超时，由调用方通过 ctx cancel 控制
		},
	}
}

func (c *Client) BaseURL() string {
	return c.baseURL
}

func (c *Client) GetMe(ctx context.Context) (Me, error) {
	var resp Me
	if err := c.doJSON(ctx, http.MethodGet, "/v1/me", nil, &resp); err != nil {
		return Me{}, fmt.Errorf("get me: %w", err)
	}
	return resp, nil
}

func (c *Client) ListSelectablePersonas(ctx context.Context) ([]Persona, error) {
	var resp []Persona
	if err := c.doJSON(ctx, http.MethodGet, "/v1/me/selectable-personas", nil, &resp); err != nil {
		return nil, fmt.Errorf("list selectable personas: %w", err)
	}
	return resp, nil
}

func (c *Client) ListLlmProviders(ctx context.Context) ([]LlmProvider, error) {
	var resp []LlmProvider
	if err := c.doJSON(ctx, http.MethodGet, "/v1/llm-providers?scope=user", nil, &resp); err != nil {
		return nil, fmt.Errorf("list llm providers: %w", err)
	}
	return resp, nil
}

func (c *Client) ListThreads(ctx context.Context, limit int) ([]Thread, error) {
	return c.ListThreadsBefore(ctx, limit, "", "")
}

func (c *Client) ListThreadsBefore(ctx context.Context, limit int, beforeCreatedAt string, beforeID string) ([]Thread, error) {
	values := url.Values{}
	if limit > 0 {
		values.Set("limit", fmt.Sprintf("%d", limit))
	}
	if beforeCreatedAt != "" {
		values.Set("before_created_at", beforeCreatedAt)
	}
	if beforeID != "" {
		values.Set("before_id", beforeID)
	}

	path := "/v1/threads"
	if encoded := values.Encode(); encoded != "" {
		path = fmt.Sprintf("%s?%s", path, encoded)
	}

	var resp []Thread
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, fmt.Errorf("list threads: %w", err)
	}
	return resp, nil
}

func (c *Client) ListAllThreads(ctx context.Context) ([]Thread, error) {
	threads := make([]Thread, 0)
	beforeCreatedAt := ""
	beforeID := ""

	for {
		page, err := c.ListThreadsBefore(ctx, ThreadPageLimit, beforeCreatedAt, beforeID)
		if err != nil {
			return nil, err
		}
		if len(page) == 0 {
			return threads, nil
		}

		threads = append(threads, page...)
		if len(page) < ThreadPageLimit {
			return threads, nil
		}

		last := page[len(page)-1]
		if last.CreatedAt == "" || last.ID == "" {
			return nil, fmt.Errorf("list threads: incomplete pagination cursor")
		}
		beforeCreatedAt = last.CreatedAt
		beforeID = last.ID
	}
}

func (c *Client) GetRun(ctx context.Context, runID string) (Run, error) {
	var resp Run
	if err := c.doJSON(ctx, http.MethodGet, "/v1/runs/"+runID, nil, &resp); err != nil {
		return Run{}, fmt.Errorf("get run: %w", err)
	}
	return resp, nil
}

// CreateThread 创建一个新 thread，返回 thread ID。
func (c *Client) CreateThread(ctx context.Context, title string, isPrivate bool) (string, error) {
	body := map[string]any{}
	if title != "" {
		body["title"] = title
	}
	if isPrivate {
		body["is_private"] = true
	}

	var resp struct {
		ID string `json:"id"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/v1/threads", body, &resp); err != nil {
		return "", fmt.Errorf("create thread: %w", err)
	}
	return resp.ID, nil
}

// AddMessage 向指定 thread 追加一条用户消息。
func (c *Client) AddMessage(ctx context.Context, threadID string, content string) error {
	body := map[string]string{"content": content}
	if err := c.doJSON(ctx, http.MethodPost, "/v1/threads/"+threadID+"/messages", body, nil); err != nil {
		return fmt.Errorf("add message: %w", err)
	}
	return nil
}

// StartRun 触发一次 run，返回 run ID。
func (c *Client) StartRun(ctx context.Context, threadID string, params RunParams) (string, error) {
	body := map[string]string{}
	if params.PersonaID != "" {
		body["persona_id"] = params.PersonaID
	}
	if params.Model != "" {
		body["model"] = params.Model
	}
	if params.WorkDir != "" {
		body["work_dir"] = params.WorkDir
	}
	if params.ReasoningMode != "" {
		body["reasoning_mode"] = params.ReasoningMode
	}

	var resp struct {
		RunID string `json:"run_id"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/v1/threads/"+threadID+"/runs", body, &resp); err != nil {
		return "", fmt.Errorf("start run: %w", err)
	}
	return resp.RunID, nil
}

// OpenEventStream 打开 SSE 事件流，返回 response body（调用方负责关闭）。
func (c *Client) OpenEventStream(ctx context.Context, runID string, afterSeq int64) (io.ReadCloser, error) {
	url := fmt.Sprintf("%s/v1/runs/%s/events?follow=true&after_seq=%d", c.baseURL, runID, afterSeq)

	resp, err := c.doAuthorized(ctx, c.sseClient, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "text/event-stream")
		return req, nil
	})
	if err != nil {
		return nil, fmt.Errorf("open event stream: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("open event stream: unexpected status %d", resp.StatusCode)
	}
	return resp.Body, nil
}

// doJSON 执行一次 JSON 请求。out 为 nil 时不解析响应体。
func (c *Client) doJSON(ctx context.Context, method, path string, in any, out any) error {
	var payload []byte
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		payload = data
	}

	resp, err := c.doAuthorized(ctx, c.httpClient, func() (*http.Request, error) {
		var bodyReader io.Reader
		if payload != nil {
			bodyReader = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
		if err != nil {
			return nil, err
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		return req, nil
	})
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("http %d: %s", resp.StatusCode, string(raw))
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// doAuthorized 带 access token 发送请求；401 时重新换取一次后重试。
func (c *Client) doAuthorized(ctx context.Context, hc *http.Client, build func() (*http.Request, error)) (*http.Response, error) {
	token, err := c.currentAccessToken(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := c.send(hc, build, token)
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		return resp, err
	}
	_ = resp.Body.Close()

	token, err = c.renewAccessToken(ctx, token)
	if err != nil {
		return nil, err
	}
	return c.send(hc, build, token)
}

func (c *Client) send(hc *http.Client, build func() (*http.Request, error), token string) (*http.Response, error) {
	req, err := build()
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	return resp, nil
}

func (c *Client) currentAccessToken(ctx context.Context) (string, error) {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	if c.accessToken != "" {
		return c.accessToken, nil
	}
	return c.exchangeLocked(ctx)
}

// renewAccessToken 只在 token 仍是失效的那一个时才重新换取，避免并发请求重复换。
func (c *Client) renewAccessToken(ctx context.Context, stale string) (string, error) {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	if c.accessToken != "" && c.accessToken != stale {
		return c.accessToken, nil
	}
	return c.exchangeLocked(ctx)
}

func (c *Client) exchangeLocked(ctx context.Context) (string, error) {
	if c.desktopToken == "" {
		return "", fmt.Errorf("local session: desktop token not found (~/.arkloop/desktop.token)")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/auth/local-session", nil)
	if err != nil {
		return "", fmt.Errorf("local session: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.desktopToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("local session: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("local session: http %d: %s", resp.StatusCode, string(raw))
	}
	var body struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("local session: decode response: %w", err)
	}
	if body.AccessToken == "" {
		return "", fmt.Errorf("local session: empty access token")
	}
	c.accessToken = body.AccessToken
	return c.accessToken, nil
}

package apiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	DefaultBaseURL = "http://127.0.0.1:19001"
	DefaultToken   = "arkloop-desktop-local-token"
)

// Client 连接 Desktop API 的 HTTP 客户端。
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client // 用于普通 JSON 请求，10s 超时
	sseClient  *http.Client // 用于 SSE 流，header 超时 10s，body 无超时
}

// RunParams 创建 run 时的可选参数，零值字段不序列化。
type RunParams struct {
	PersonaID     string
	Model         string
	WorkDir       string
	ReasoningMode string
}

// NewClient 构造客户端，baseURL 和 token 必须非空。
func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
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

// CreateThread 创建一个新 thread，返回 thread ID。
func (c *Client) CreateThread(ctx context.Context, title string) (string, error) {
	body := map[string]any{}
	if title != "" {
		body["title"] = title
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("open event stream: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.sseClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("open event stream: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("open event stream: unexpected status %d", resp.StatusCode)
	}
	return resp.Body, nil
}

// doJSON 执行一次 JSON 请求。out 为 nil 时不解析响应体。
func (c *Client) doJSON(ctx context.Context, method, path string, in any, out any) error {
	var bodyReader io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

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

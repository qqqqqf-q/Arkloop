package qqbotclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const (
	defaultTokenURL    = "https://bots.qq.com/app/getAppAccessToken"
	defaultAPIBase     = "https://api.sgroup.qq.com"
	defaultHTTPTimeout = 10 * time.Second
)

type ClientOptions struct {
	HTTPClient *http.Client
	TokenURL   string
	APIBase    string
}

type Client struct {
	creds Credentials
	http  *http.Client

	tokenURL string
	apiBase  string

	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
	msgSeq      uint64
}

func NewClient(creds Credentials, opts *ClientOptions) *Client {
	httpClient := &http.Client{Timeout: defaultHTTPTimeout}
	tokenURL := defaultTokenURL
	apiBase := defaultAPIBase
	if opts != nil {
		if opts.HTTPClient != nil {
			httpClient = opts.HTTPClient
		}
		if strings.TrimSpace(opts.TokenURL) != "" {
			tokenURL = strings.TrimRight(strings.TrimSpace(opts.TokenURL), "/")
		}
		if strings.TrimSpace(opts.APIBase) != "" {
			apiBase = strings.TrimRight(strings.TrimSpace(opts.APIBase), "/")
		}
	}
	return &Client{
		creds:    creds,
		http:     httpClient,
		tokenURL: tokenURL,
		apiBase:  apiBase,
	}
}

func (c *Client) GetAccessToken(ctx context.Context) (string, error) {
	if c == nil {
		return "", fmt.Errorf("qqbot client is nil")
	}
	c.mu.Lock()
	if c.accessToken != "" && time.Until(c.expiresAt) > time.Minute {
		token := c.accessToken
		c.mu.Unlock()
		return token, nil
	}
	c.mu.Unlock()

	payload := map[string]string{
		"appId":        strings.TrimSpace(c.creds.AppID),
		"clientSecret": strings.TrimSpace(c.creds.ClientSecret),
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("qqbot token status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	token := firstJSONText(parsed, "access_token", "accessToken", "app_access_token", "appAccessToken")
	if token == "" {
		return "", fmt.Errorf("qqbot token response missing access_token")
	}
	expiresIn := firstJSONInt(parsed, 7200, "expires_in", "expiresIn")
	if expiresIn <= 0 {
		expiresIn = 7200
	}

	c.mu.Lock()
	c.accessToken = token
	c.expiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
	c.mu.Unlock()
	return token, nil
}

func (c *Client) GetGatewayURL(ctx context.Context) (string, error) {
	token, err := c.GetAccessToken(ctx)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiBase+"/gateway", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "QQBot "+token)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("qqbot gateway status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if strings.TrimSpace(out.URL) == "" {
		return "", fmt.Errorf("qqbot gateway response missing url")
	}
	return strings.TrimSpace(out.URL), nil
}

func (c *Client) SendText(ctx context.Context, scope, target, content, msgID string) (*SendMessageResponse, error) {
	scope = strings.ToLower(strings.TrimSpace(scope))
	target = strings.TrimSpace(target)
	content = strings.TrimSpace(content)
	if target == "" {
		return nil, fmt.Errorf("qqbot send target is required")
	}
	if content == "" {
		return nil, fmt.Errorf("qqbot send content is required")
	}

	path := ""
	switch scope {
	case ScopeC2C, "private", "dm":
		path = "/v2/users/" + target + "/messages"
	case ScopeGroup:
		path = "/v2/groups/" + target + "/messages"
	default:
		return nil, fmt.Errorf("unsupported qqbot message scope: %s", scope)
	}

	token, err := c.GetAccessToken(ctx)
	if err != nil {
		return nil, err
	}
	payload := map[string]any{
		"msg_type": 0,
		"content":  content,
		"msg_seq":  c.nextMsgSeq(),
	}
	if strings.TrimSpace(msgID) != "" {
		payload["msg_id"] = strings.TrimSpace(msgID)
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBase+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "QQBot "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("qqbot send status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out SendMessageResponse
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	out.Raw = raw
	return &out, nil
}

func (c *Client) nextMsgSeq() int {
	counter := atomic.AddUint64(&c.msgSeq, 1)
	return int((counter-1)%65535) + 1
}

type GatewayListenerOptions struct {
	Intents int
	Logger  *slog.Logger
}

type GatewayListener struct {
	client  *Client
	handler GatewayEventHandler
	intents int
	logger  *slog.Logger
}

func NewGatewayListener(client *Client, handler GatewayEventHandler, opts *GatewayListenerOptions) *GatewayListener {
	intents := DefaultIntents
	logger := slog.Default()
	if opts != nil {
		if opts.Intents != 0 {
			intents = opts.Intents
		}
		if opts.Logger != nil {
			logger = opts.Logger
		}
	}
	return &GatewayListener{
		client:  client,
		handler: handler,
		intents: intents,
		logger:  logger,
	}
}

func (l *GatewayListener) Run(ctx context.Context) error {
	if l == nil || l.client == nil {
		return fmt.Errorf("qqbot gateway listener is not configured")
	}
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := l.connectAndRead(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			l.logger.WarnContext(ctx, "qqbot_gateway_disconnected", "err", err.Error())
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (l *GatewayListener) connectAndRead(ctx context.Context) error {
	gatewayURL, err := l.client.GetGatewayURL(ctx)
	if err != nil {
		return err
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, gatewayURL, nil)
	if err != nil {
		return fmt.Errorf("qqbot gateway dial: %w", err)
	}
	defer func() { _ = conn.Close() }()

	var heartbeatCancel context.CancelFunc
	defer func() {
		if heartbeatCancel != nil {
			heartbeatCancel()
		}
	}()

	var lastSeq any
	var lastSeqMu sync.Mutex
	setLastSeq := func(seq any) {
		lastSeqMu.Lock()
		lastSeq = seq
		lastSeqMu.Unlock()
	}
	getLastSeq := func() any {
		lastSeqMu.Lock()
		defer lastSeqMu.Unlock()
		return lastSeq
	}
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		var frame struct {
			Op int             `json:"op"`
			S  *int64          `json:"s,omitempty"`
			T  string          `json:"t,omitempty"`
			D  json.RawMessage `json:"d,omitempty"`
		}
		if err := json.Unmarshal(raw, &frame); err != nil {
			continue
		}
		if frame.S != nil {
			setLastSeq(*frame.S)
		}
		switch frame.Op {
		case 10:
			interval := heartbeatInterval(frame.D)
			hbCtx, cancel := context.WithCancel(ctx)
			heartbeatCancel = cancel
			if err := l.identify(ctx, conn); err != nil {
				return err
			}
			go l.heartbeatLoop(hbCtx, conn, interval, getLastSeq)
		case 0:
			seq := int64(0)
			if frame.S != nil {
				seq = *frame.S
			}
			if l.handler != nil {
				l.handler(ctx, GatewayEvent{Op: frame.Op, Type: strings.TrimSpace(frame.T), Sequence: seq, Data: frame.D})
			}
		case 7, 9:
			return fmt.Errorf("qqbot gateway reconnect requested")
		}
	}
}

func (l *GatewayListener) identify(ctx context.Context, conn *websocket.Conn) error {
	token, err := l.client.GetAccessToken(ctx)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"op": 2,
		"d": map[string]any{
			"token":   "QQBot " + token,
			"intents": l.intents,
			"shard":   []int{0, 1},
			"properties": map[string]string{
				"$os":      "linux",
				"$browser": "arkloop",
				"$device":  "arkloop",
			},
		},
	}
	return conn.WriteJSON(payload)
}

func (l *GatewayListener) heartbeatLoop(ctx context.Context, conn *websocket.Conn, interval time.Duration, lastSeq func() any) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			seq := any(nil)
			if lastSeq != nil {
				seq = lastSeq()
			}
			if err := conn.WriteJSON(map[string]any{"op": 1, "d": seq}); err != nil {
				return
			}
		}
	}
}

func heartbeatInterval(raw json.RawMessage) time.Duration {
	var hello struct {
		HeartbeatInterval int64 `json:"heartbeat_interval"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &hello) != nil || hello.HeartbeatInterval <= 0 {
		return 30 * time.Second
	}
	return time.Duration(hello.HeartbeatInterval) * time.Millisecond
}

func firstJSONText(values map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		raw := values[key]
		if len(raw) == 0 {
			continue
		}
		var text string
		if json.Unmarshal(raw, &text) == nil && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func firstJSONInt(values map[string]json.RawMessage, fallback int64, keys ...string) int64 {
	for _, key := range keys {
		raw := values[key]
		if len(raw) == 0 {
			continue
		}
		var n int64
		if json.Unmarshal(raw, &n) == nil {
			return n
		}
		var s string
		if json.Unmarshal(raw, &s) == nil {
			var parsed int64
			if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &parsed); err == nil {
				return parsed
			}
		}
	}
	return fallback
}

package onebotclient

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WSListener 作为 WebSocket 客户端连接 OneBot11 WS Server，持续读取事件。
type WSListener struct {
	url      string
	token    string
	handler  func(ctx context.Context, event Event)
	logger   *slog.Logger

	mu     sync.Mutex
	conn   *websocket.Conn
	cancel context.CancelFunc
}

func NewWSListener(url, token string, handler func(ctx context.Context, event Event), logger *slog.Logger) *WSListener {
	if logger == nil {
		logger = slog.Default()
	}
	return &WSListener{
		url:     strings.TrimSpace(url),
		token:   strings.TrimSpace(token),
		handler: handler,
		logger:  logger,
	}
}

// Start 在后台 goroutine 中连接并读取事件，支持断线重连。
func (l *WSListener) Start(ctx context.Context) {
	loopCtx, cancel := context.WithCancel(ctx)
	l.mu.Lock()
	l.cancel = cancel
	l.mu.Unlock()

	go l.loop(loopCtx)
}

func (l *WSListener) Stop() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cancel != nil {
		l.cancel()
		l.cancel = nil
	}
	if l.conn != nil {
		_ = l.conn.Close()
		l.conn = nil
	}
}

func (l *WSListener) loop(ctx context.Context) {
	backoff := time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		err := l.connectAndRead(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			l.logger.Warn("onebot_ws_disconnected", "url", l.url, "error", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		backoff = min(backoff*2, maxBackoff)
	}
}

func (l *WSListener) connectAndRead(ctx context.Context) error {
	header := http.Header{}
	if l.token != "" {
		header.Set("Authorization", "Bearer "+l.token)
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	conn, _, err := dialer.DialContext(ctx, l.url, header)
	if err != nil {
		return fmt.Errorf("ws dial: %w", err)
	}

	l.mu.Lock()
	l.conn = conn
	l.mu.Unlock()

	defer func() {
		_ = conn.Close()
		l.mu.Lock()
		if l.conn == conn {
			l.conn = nil
		}
		l.mu.Unlock()
	}()

	l.logger.Info("onebot_ws_connected", "url", l.url)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("ws read: %w", err)
		}

		var event Event
		if err := json.Unmarshal(message, &event); err != nil {
			l.logger.Debug("onebot_ws_parse_error", "error", err)
			continue
		}

		if event.IsHeartbeat() || event.IsLifecycle() {
			continue
		}

		if l.handler != nil {
			l.handler(ctx, event)
		}
	}
}

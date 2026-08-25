package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	headerContentLength = "Content-Length"
	headerSeparator     = "\r\n\r\n"
	maxMessageSize      = 10 * 1024 * 1024 // 10MB
)

type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (r *RPCError) Error() string {
	return fmt.Sprintf("LSP error %d: %s", r.Code, r.Message)
}

type notificationHandler struct {
	id int64
	fn func(method string, params json.RawMessage)
}

type NotificationHandlerRegistry struct {
	mu       sync.RWMutex
	handlers map[string][]notificationHandler
	nextSub  atomic.Int64
}

func newNotificationHandlerRegistry() *NotificationHandlerRegistry {
	return &NotificationHandlerRegistry{
		handlers: make(map[string][]notificationHandler),
	}
}

func (r *NotificationHandlerRegistry) subscribe(method string, fn func(string, json.RawMessage)) func() {
	id := r.nextSub.Add(1)
	r.mu.Lock()
	r.handlers[method] = append(r.handlers[method], notificationHandler{id: id, fn: fn})
	r.mu.Unlock()

	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		hs := r.handlers[method]
		for i, h := range hs {
			if h.id == id {
				r.handlers[method] = append(hs[:i], hs[i+1:]...)
				return
			}
		}
	}
}

func (r *NotificationHandlerRegistry) dispatch(method string, params json.RawMessage) {
	r.mu.RLock()
	hs := make([]notificationHandler, len(r.handlers[method]))
	copy(hs, r.handlers[method])
	r.mu.RUnlock()

	for _, h := range hs {
		h.fn(method, params)
	}
}

type Transport struct {
	mu       sync.Mutex
	stdin    io.WriteCloser
	stdout   io.ReadCloser
	reader   *bufio.Reader
	pending  map[int64]chan *Message
	pendMu   sync.Mutex
	handlers *NotificationHandlerRegistry
	nextID   atomic.Int64
	closed   atomic.Bool
}

func NewTransport(stdin io.WriteCloser, stdout io.ReadCloser) *Transport {
	return &Transport{
		stdin:    stdin,
		stdout:   stdout,
		reader:   bufio.NewReader(stdout),
		pending:  make(map[int64]chan *Message),
		handlers: newNotificationHandlerRegistry(),
	}
}

func (t *Transport) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal params: %w", err)
	}

	id := t.nextID.Add(1)
	ch := make(chan *Message, 1)

	t.pendMu.Lock()
	t.pending[id] = ch
	t.pendMu.Unlock()

	msg := &Message{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  method,
		Params:  raw,
	}
	if err := t.writeMessage(msg); err != nil {
		t.pendMu.Lock()
		delete(t.pending, id)
		t.pendMu.Unlock()
		return nil, err
	}

	select {
	case resp := <-ch:
		if resp == nil {
			return nil, fmt.Errorf("transport closed")
		}
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	case <-ctx.Done():
		t.pendMu.Lock()
		delete(t.pending, id)
		t.pendMu.Unlock()
		// ch is buffered(1), so if handleMessages sends a response after removal,
		// the send won't block and ch will be GC'd with no goroutine leak.
		return nil, ctx.Err()
	}
}

func (t *Transport) Notify(_ context.Context, method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("marshal params: %w", err)
	}

	msg := &Message{
		JSONRPC: "2.0",
		Method:  method,
		Params:  raw,
	}
	return t.writeMessage(msg)
}

func (t *Transport) Subscribe(method string, fn func(string, json.RawMessage)) func() {
	return t.handlers.subscribe(method, fn)
}

func (t *Transport) readMessage() (*Message, error) {
	contentLen := -1
	for {
		line, err := t.reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		parts := strings.SplitN(line, ": ", 2)
		if len(parts) == 2 && parts[0] == headerContentLength {
			contentLen, err = strconv.Atoi(parts[1])
			if err != nil {
				return nil, fmt.Errorf("invalid Content-Length %q: %w", parts[1], err)
			}
		}
	}

	if contentLen < 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}
	if contentLen > maxMessageSize {
		return nil, fmt.Errorf("message size %d exceeds limit %d", contentLen, maxMessageSize)
	}

	body := make([]byte, contentLen)
	if _, err := io.ReadFull(t.reader, body); err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	var msg Message
	if err := json.Unmarshal(body, &msg); err != nil {
		return nil, fmt.Errorf("unmarshal message: %w", err)
	}
	return &msg, nil
}

func (t *Transport) writeMessage(msg *Message) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	header := fmt.Sprintf("%s: %d%s", headerContentLength, len(body), headerSeparator)

	t.mu.Lock()
	defer t.mu.Unlock()

	if _, err := io.WriteString(t.stdin, header); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	if _, err := t.stdin.Write(body); err != nil {
		return fmt.Errorf("write body: %w", err)
	}
	return nil
}

func (t *Transport) handleMessages() {
	for {
		msg, err := t.readMessage()
		if err != nil {
			t.pendMu.Lock()
			for id, ch := range t.pending {
				close(ch)
				delete(t.pending, id)
			}
			t.pendMu.Unlock()
			return
		}

		switch {
		case msg.ID != nil && msg.Method == "":
			// response to a pending call
			t.pendMu.Lock()
			ch, ok := t.pending[*msg.ID]
			if ok {
				delete(t.pending, *msg.ID)
			}
			t.pendMu.Unlock()
			if ok {
				ch <- msg
			}

		case msg.ID != nil && msg.Method != "":
			// server request: respond based on method, then dispatch
			var result json.RawMessage
			switch msg.Method {
			case "workspace/configuration":
				// return array of empty objects matching requested items count
				var p struct {
					Items []any `json:"items"`
				}
				n := 1
				if json.Unmarshal(msg.Params, &p) == nil && len(p.Items) > 0 {
					n = len(p.Items)
				}
				items := make([]json.RawMessage, n)
				for i := range items {
					items[i] = json.RawMessage("{}")
				}
				result, _ = json.Marshal(items)
			default:
				// client/registerCapability and unknown methods: success with null
				result = json.RawMessage("null")
			}
			resp := &Message{
				JSONRPC: "2.0",
				ID:      msg.ID,
				Result:  result,
			}
			_ = t.writeMessage(resp)
			t.handlers.dispatch(msg.Method, msg.Params)

		default:
			// notification
			t.handlers.dispatch(msg.Method, msg.Params)
		}
	}
}

func (t *Transport) Close() error {
	t.closed.Store(true)
	_ = t.stdin.Close()
	return t.stdout.Close()
}

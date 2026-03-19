package telegrambot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type Client struct {
	baseURL    string
	httpClient HTTPClient
}

func NewClient(baseURL string, httpClient HTTPClient) *Client {
	base := strings.TrimSpace(baseURL)
	if base == "" {
		base = "https://api.telegram.org"
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{
		baseURL:    strings.TrimRight(base, "/"),
		httpClient: httpClient,
	}
}

type SetWebhookRequest struct {
	URL         string   `json:"url"`
	SecretToken string   `json:"secret_token,omitempty"`
	Updates     []string `json:"allowed_updates,omitempty"`
}

type BotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

type SendMessageRequest struct {
	ChatID           string `json:"chat_id"`
	Text             string `json:"text"`
	ParseMode        string `json:"parse_mode,omitempty"`
	ReplyToMessageID string `json:"reply_to_message_id,omitempty"`
	MessageThreadID  string `json:"message_thread_id,omitempty"`
}

type SentMessage struct {
	MessageID int64            `json:"message_id"`
	Chat      *SentMessageChat `json:"chat,omitempty"`
}

type SentMessageChat struct {
	ID int64 `json:"id"`
}

type GetUpdatesRequest struct {
	Offset         *int64   `json:"offset,omitempty"`
	Limit          int      `json:"limit,omitempty"`
	TimeoutSeconds int      `json:"timeout,omitempty"`
	Updates        []string `json:"allowed_updates,omitempty"`
}

type apiEnvelope struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description"`
	Result      json.RawMessage `json:"result"`
}

func (c *Client) SetWebhook(ctx context.Context, token string, req SetWebhookRequest) error {
	return c.callJSON(ctx, token, "setWebhook", req, nil)
}

func (c *Client) DeleteWebhook(ctx context.Context, token string) error {
	return c.callJSON(ctx, token, "deleteWebhook", map[string]any{}, nil)
}

func (c *Client) SetMyCommands(ctx context.Context, token string, commands []BotCommand) error {
	return c.callJSON(ctx, token, "setMyCommands", map[string]any{"commands": commands}, nil)
}

func (c *Client) SendMessage(ctx context.Context, token string, req SendMessageRequest) (*SentMessage, error) {
	var result SentMessage
	if err := c.callJSON(ctx, token, "sendMessage", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) GetUpdates(ctx context.Context, token string, req GetUpdatesRequest, out any) error {
	return c.callJSON(ctx, token, "getUpdates", req, out)
}

type BotInfo struct {
	ID        int64   `json:"id"`
	IsBot     bool    `json:"is_bot"`
	FirstName string  `json:"first_name"`
	Username  *string `json:"username"`
}

func (c *Client) GetMe(ctx context.Context, token string) (*BotInfo, error) {
	var info BotInfo
	if err := c.callJSON(ctx, token, "getMe", map[string]any{}, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (c *Client) callJSON(ctx context.Context, token string, method string, body any, out any) error {
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("telegrambot: token must not be empty")
	}
	if strings.TrimSpace(method) == "" {
		return fmt.Errorf("telegrambot: method must not be empty")
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("telegrambot: marshal %s: %w", method, err)
	}
	endpoint := fmt.Sprintf("%s/bot%s/%s", c.baseURL, url.PathEscape(token), method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("telegrambot: new request %s: %s", method, redactToken(err.Error(), token))
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("telegrambot: call %s: %s", method, redactToken(err.Error(), token))
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("telegrambot: read %s: %w", method, err)
	}
	var envelope apiEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("telegrambot: decode %s: %w", method, err)
	}
	if resp.StatusCode >= 400 || !envelope.OK {
		if strings.TrimSpace(envelope.Description) == "" {
			envelope.Description = strings.TrimSpace(string(raw))
		}
		if envelope.Description == "" {
			envelope.Description = resp.Status
		}
		return fmt.Errorf("telegrambot: %s failed: %s", method, envelope.Description)
	}
	if out != nil && len(envelope.Result) > 0 {
		if err := json.Unmarshal(envelope.Result, out); err != nil {
			return fmt.Errorf("telegrambot: decode result %s: %w", method, err)
		}
	}
	return nil
}

func redactToken(text, token string) string {
	cleaned := strings.TrimSpace(text)
	if cleaned == "" || strings.TrimSpace(token) == "" {
		return cleaned
	}
	return strings.ReplaceAll(cleaned, token, "[REDACTED]")
}

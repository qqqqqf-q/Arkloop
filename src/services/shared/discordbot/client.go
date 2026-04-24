package discordbot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const DefaultAPIBaseURL = "https://discord.com/api/v10"

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
		base = DefaultAPIBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{
		baseURL:    strings.TrimRight(base, "/"),
		httpClient: httpClient,
	}
}

type CurrentUser struct {
	ID       string  `json:"id"`
	Username string  `json:"username"`
	Bot      bool    `json:"bot"`
	Avatar   *string `json:"avatar"`
}

type CurrentApplication struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type VerifiedBot struct {
	ApplicationID   string
	ApplicationName string
	BotUserID       string
	BotUsername     string
}

type CreateDMChannelRequest struct {
	RecipientID string `json:"recipient_id"`
}

type DMChannel struct {
	ID string `json:"id"`
}

type MessageReference struct {
	MessageID string `json:"message_id"`
}

type CreateMessageRequest struct {
	Content          string            `json:"content"`
	MessageReference *MessageReference `json:"message_reference,omitempty"`
}

type SentMessage struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
}

type ApplicationCommand struct {
	Name        string                          `json:"name"`
	Description string                          `json:"description"`
	Type        int                             `json:"type,omitempty"`
	Options     []ApplicationCommandOption      `json:"options,omitempty"`
}

type ApplicationCommandOption struct {
	Type        int    `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required,omitempty"`
}

func (c *Client) VerifyBot(ctx context.Context, token string) (*VerifiedBot, error) {
	user, err := c.GetCurrentUser(ctx, token)
	if err != nil {
		return nil, err
	}
	app, err := c.GetCurrentApplication(ctx, token)
	if err != nil {
		return nil, err
	}
	return &VerifiedBot{
		ApplicationID:   strings.TrimSpace(app.ID),
		ApplicationName: strings.TrimSpace(app.Name),
		BotUserID:       strings.TrimSpace(user.ID),
		BotUsername:     strings.TrimSpace(user.Username),
	}, nil
}

func (c *Client) GetCurrentUser(ctx context.Context, token string) (*CurrentUser, error) {
	var user CurrentUser
	if err := c.callJSON(ctx, token, http.MethodGet, "/users/@me", nil, &user); err != nil {
		return nil, err
	}
	return &user, nil
}

func (c *Client) GetCurrentApplication(ctx context.Context, token string) (*CurrentApplication, error) {
	var app CurrentApplication
	if err := c.callJSON(ctx, token, http.MethodGet, "/applications/@me", nil, &app); err != nil {
		return nil, err
	}
	return &app, nil
}

func (c *Client) CreateDMChannel(ctx context.Context, token string, recipientID string) (*DMChannel, error) {
	var channel DMChannel
	req := CreateDMChannelRequest{RecipientID: strings.TrimSpace(recipientID)}
	if err := c.callJSON(ctx, token, http.MethodPost, "/users/@me/channels", req, &channel); err != nil {
		return nil, err
	}
	return &channel, nil
}

func (c *Client) SendMessage(ctx context.Context, token string, channelID string, req CreateMessageRequest) (*SentMessage, error) {
	path := fmt.Sprintf("/channels/%s/messages", strings.TrimSpace(channelID))
	var message SentMessage
	if err := c.callJSON(ctx, token, http.MethodPost, path, req, &message); err != nil {
		return nil, err
	}
	return &message, nil
}

func (c *Client) RegisterGlobalCommands(ctx context.Context, token string, applicationID string, commands []ApplicationCommand) error {
	path := fmt.Sprintf("/applications/%s/commands", strings.TrimSpace(applicationID))
	return c.callJSON(ctx, token, http.MethodPut, path, commands, nil)
}

func (c *Client) RegisterGuildCommands(ctx context.Context, token string, applicationID string, guildID string, commands []ApplicationCommand) error {
	path := fmt.Sprintf("/applications/%s/guilds/%s/commands", strings.TrimSpace(applicationID), strings.TrimSpace(guildID))
	return c.callJSON(ctx, token, http.MethodPut, path, commands, nil)
}

func (c *Client) callJSON(ctx context.Context, token string, method string, path string, payload any, out any) error {
	if c == nil {
		return fmt.Errorf("discordbot: client must not be nil")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("discordbot: token must not be empty")
	}
	if strings.TrimSpace(method) == "" {
		return fmt.Errorf("discordbot: method must not be empty")
	}
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("discordbot: path must not be empty")
	}

	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("discordbot: marshal payload: %w", err)
		}
		body = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("discordbot: new request: %w", err)
	}
	req.Header.Set("Authorization", "Bot "+token)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("discordbot: do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("discordbot: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discordbot: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("discordbot: decode response: %w", err)
	}
	return nil
}

package telegrambot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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

// SendChatActionRequest mirrors Telegram sendChatAction (chat_id + action).
type SendChatActionRequest struct {
	ChatID string `json:"chat_id"`
	Action string `json:"action"`
}

// MessageReactionEmoji is one element of setMessageReaction.reaction.
type MessageReactionEmoji struct {
	Type  string `json:"type"`
	Emoji string `json:"emoji,omitempty"`
}

// SetMessageReactionRequest mirrors Telegram setMessageReaction.
type SetMessageReactionRequest struct {
	ChatID    string                 `json:"chat_id"`
	MessageID int64                  `json:"message_id"`
	Reaction  []MessageReactionEmoji `json:"reaction"`
}

// EditMessageTextRequest mirrors Telegram editMessageText (for future streaming).
type EditMessageTextRequest struct {
	ChatID          string `json:"chat_id"`
	MessageID       int64  `json:"message_id"`
	Text            string `json:"text"`
	ParseMode       string `json:"parse_mode,omitempty"`
	MessageThreadID string `json:"message_thread_id,omitempty"`
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

// SendMessageWithHTMLFallback 先按 req.ParseMode 发送；若为 HTML 实体解析错误则同一条内容降级为纯文本再发一次。
func (c *Client) SendMessageWithHTMLFallback(ctx context.Context, token string, req SendMessageRequest) (*SentMessage, error) {
	sent, err := c.SendMessage(ctx, token, req)
	if err == nil || !IsTelegramEntityParseError(err) || strings.TrimSpace(req.ParseMode) == "" {
		return sent, err
	}
	plain := StripTelegramHTMLToPlain(req.Text)
	if strings.TrimSpace(plain) == "" {
		return nil, err
	}
	retry := req
	retry.ParseMode = ""
	retry.Text = plain
	return c.SendMessage(ctx, token, retry)
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

type GetChatMemberRequest struct {
	ChatID string `json:"chat_id"`
	UserID int64  `json:"user_id"`
}

// ChatMemberStatus: "creator" | "administrator" | "member" | "restricted" | "left" | "kicked"
type ChatMemberInfo struct {
	Status string `json:"status"`
}

func (c *Client) GetChatMember(ctx context.Context, token string, req GetChatMemberRequest) (*ChatMemberInfo, error) {
	var info ChatMemberInfo
	if err := c.callJSON(ctx, token, "getChatMember", req, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// GetMe reports whether the bot can perform the operation.
func (c *Client) GetMe(ctx context.Context, token string) (*BotInfo, error) {
	var info BotInfo
	if err := c.callJSON(ctx, token, "getMe", map[string]any{}, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// SendChatAction posts a chat action (e.g. action "typing").
func (c *Client) SendChatAction(ctx context.Context, token string, req SendChatActionRequest) error {
	var ok bool
	if err := c.callJSON(ctx, token, "sendChatAction", req, &ok); err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("telegrambot: sendChatAction returned non-true result")
	}
	return nil
}

// SetMessageReaction sets emoji reactions on a message (empty reaction clears bot reactions).
func (c *Client) SetMessageReaction(ctx context.Context, token string, req SetMessageReactionRequest) error {
	var ok bool
	if err := c.callJSON(ctx, token, "setMessageReaction", req, &ok); err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("telegrambot: setMessageReaction returned non-true result")
	}
	return nil
}

// EditMessageText updates an existing message (optional streaming UX).
// Telegram returns either a Message or true; we only require ok envelope.
func (c *Client) EditMessageText(ctx context.Context, token string, req EditMessageTextRequest) error {
	return c.callJSON(ctx, token, "editMessageText", req, nil)
}

// isLocalPath returns true if the path looks like a local filesystem path.
func isLocalPath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	// Unix absolute path, home dir, or Windows drive letter
	return strings.HasPrefix(path, "/") || strings.HasPrefix(path, "~/") ||
		(len(path) >= 2 && path[1] == ':')
}

// sendMediaMultipart uploads a local file using multipart/form-data.
func (c *Client) sendMediaMultipart(ctx context.Context, token, method, fieldName, chatID, filePath, caption, parseMode, messageThreadID string) (*SentMessage, error) {
	if strings.HasPrefix(filePath, "~/") {
		home := os.Getenv("HOME")
		if home != "" {
			filePath = filepath.Join(home, filePath[2:])
		}
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("telegrambot: open file %q: %w", filePath, err)
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile(fieldName, filepath.Base(filePath))
	if err != nil {
		return nil, fmt.Errorf("telegrambot: create form file: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, fmt.Errorf("telegrambot: copy file content: %w", err)
	}

	// Add fields
	if err := writer.WriteField("chat_id", chatID); err != nil {
		return nil, fmt.Errorf("telegrambot: write chat_id: %w", err)
	}
	if caption != "" {
		if err := writer.WriteField("caption", caption); err != nil {
			return nil, fmt.Errorf("telegrambot: write caption: %w", err)
		}
	}
	if parseMode != "" {
		if err := writer.WriteField("parse_mode", parseMode); err != nil {
			return nil, fmt.Errorf("telegrambot: write parse_mode: %w", err)
		}
	}
	if messageThreadID != "" {
		if err := writer.WriteField("message_thread_id", messageThreadID); err != nil {
			return nil, fmt.Errorf("telegrambot: write message_thread_id: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("telegrambot: close writer: %w", err)
	}

	endpoint := fmt.Sprintf("%s/bot%s/%s", c.baseURL, url.PathEscape(token), method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body.Bytes()))
	if err != nil {
		return nil, fmt.Errorf("telegrambot: new request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("telegrambot: multipart upload: %w", err)
	}
	defer resp.Body.Close()

	return c.parseSentMessageResp(resp)
}

func (c *Client) parseSentMessageResp(resp *http.Response) (*SentMessage, error) {
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("telegrambot: upload failed status %d: %s", resp.StatusCode, string(raw))
	}

	var envelope apiEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("telegrambot: decode response: %w", err)
	}
	if !envelope.OK {
		return nil, fmt.Errorf("telegrambot: upload failed: %s", envelope.Description)
	}
	var result SentMessage
	if err := json.Unmarshal(envelope.Result, &result); err != nil {
		return nil, fmt.Errorf("telegrambot: unmarshal result: %w", err)
	}
	return &result, nil
}

// SendPhoto sends a photo by URL, file_id, or local file path.
func (c *Client) SendPhoto(ctx context.Context, token string, chatID, photo, caption, parseMode, messageThreadID string) (*SentMessage, error) {
	if isLocalPath(photo) {
		return c.sendMediaMultipart(ctx, token, "sendPhoto", "photo", chatID, photo, caption, parseMode, messageThreadID)
	}
	req := map[string]any{"chat_id": chatID, "photo": photo}
	if caption != "" {
		req["caption"] = caption
	}
	if parseMode != "" {
		req["parse_mode"] = parseMode
	}
	if messageThreadID != "" {
		req["message_thread_id"] = messageThreadID
	}
	var result SentMessage
	if err := c.callJSON(ctx, token, "sendPhoto", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SendDocument sends a document by URL, file_id, or local file path.
func (c *Client) SendDocument(ctx context.Context, token string, chatID, document, caption, parseMode, messageThreadID string) (*SentMessage, error) {
	if isLocalPath(document) {
		return c.sendMediaMultipart(ctx, token, "sendDocument", "document", chatID, document, caption, parseMode, messageThreadID)
	}
	req := map[string]any{"chat_id": chatID, "document": document}
	if caption != "" {
		req["caption"] = caption
	}
	if parseMode != "" {
		req["parse_mode"] = parseMode
	}
	if messageThreadID != "" {
		req["message_thread_id"] = messageThreadID
	}
	var result SentMessage
	if err := c.callJSON(ctx, token, "sendDocument", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SendAudio sends an audio file by URL, file_id, or local file path.
func (c *Client) SendAudio(ctx context.Context, token string, chatID, audio, caption, parseMode, messageThreadID string) (*SentMessage, error) {
	if isLocalPath(audio) {
		return c.sendMediaMultipart(ctx, token, "sendAudio", "audio", chatID, audio, caption, parseMode, messageThreadID)
	}
	req := map[string]any{"chat_id": chatID, "audio": audio}
	if caption != "" {
		req["caption"] = caption
	}
	if parseMode != "" {
		req["parse_mode"] = parseMode
	}
	if messageThreadID != "" {
		req["message_thread_id"] = messageThreadID
	}
	var result SentMessage
	if err := c.callJSON(ctx, token, "sendAudio", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SendVideo sends a video by URL, file_id, or local file path.
func (c *Client) SendVideo(ctx context.Context, token string, chatID, video, caption, parseMode, messageThreadID string) (*SentMessage, error) {
	if isLocalPath(video) {
		return c.sendMediaMultipart(ctx, token, "sendVideo", "video", chatID, video, caption, parseMode, messageThreadID)
	}
	req := map[string]any{"chat_id": chatID, "video": video}
	if caption != "" {
		req["caption"] = caption
	}
	if parseMode != "" {
		req["parse_mode"] = parseMode
	}
	if messageThreadID != "" {
		req["message_thread_id"] = messageThreadID
	}
	var result SentMessage
	if err := c.callJSON(ctx, token, "sendVideo", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SendVoice sends a voice note by URL, file_id, or local file path.
func (c *Client) SendVoice(ctx context.Context, token string, chatID, voice, caption, parseMode, messageThreadID string) (*SentMessage, error) {
	if isLocalPath(voice) {
		return c.sendMediaMultipart(ctx, token, "sendVoice", "voice", chatID, voice, caption, parseMode, messageThreadID)
	}
	req := map[string]any{"chat_id": chatID, "voice": voice}
	if caption != "" {
		req["caption"] = caption
	}
	if parseMode != "" {
		req["parse_mode"] = parseMode
	}
	if messageThreadID != "" {
		req["message_thread_id"] = messageThreadID
	}
	var result SentMessage
	if err := c.callJSON(ctx, token, "sendVoice", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SendAnimation sends an animation (GIF) by URL, file_id, or local file path.
func (c *Client) SendAnimation(ctx context.Context, token string, chatID, animation, caption, parseMode, messageThreadID string) (*SentMessage, error) {
	if isLocalPath(animation) {
		return c.sendMediaMultipart(ctx, token, "sendAnimation", "animation", chatID, animation, caption, parseMode, messageThreadID)
	}
	req := map[string]any{"chat_id": chatID, "animation": animation}
	if caption != "" {
		req["caption"] = caption
	}
	if parseMode != "" {
		req["parse_mode"] = parseMode
	}
	if messageThreadID != "" {
		req["message_thread_id"] = messageThreadID
	}
	var result SentMessage
	if err := c.callJSON(ctx, token, "sendAnimation", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
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

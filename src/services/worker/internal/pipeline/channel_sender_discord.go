package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

const defaultDiscordAPIBaseURL = "https://discord.com/api/v10"

type DiscordHTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type DiscordChannelSender struct {
	baseURL      string
	client       DiscordHTTPDoer
	token        string
	segmentDelay time.Duration
}

type discordCreateMessageRequest struct {
	Content          string                        `json:"content"`
	MessageReference *discordMessageReferenceInput `json:"message_reference,omitempty"`
}

type discordMessageReferenceInput struct {
	MessageID string `json:"message_id"`
}

type discordCreateMessageResponse struct {
	ID string `json:"id"`
}

type discordErrorResponse struct {
	Message string `json:"message"`
}

func NewDiscordChannelSender(token string) *DiscordChannelSender {
	return NewDiscordChannelSenderWithClient(nil, os.Getenv("ARKLOOP_DISCORD_API_BASE_URL"), token, resolveSegmentDelay())
}

func NewDiscordChannelSenderWithClient(client DiscordHTTPDoer, baseURL, token string, segmentDelay time.Duration) *DiscordChannelSender {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	base := strings.TrimSpace(baseURL)
	if base == "" {
		base = defaultDiscordAPIBaseURL
	}
	return &DiscordChannelSender{
		baseURL:      strings.TrimRight(base, "/"),
		client:       client,
		token:        strings.TrimSpace(token),
		segmentDelay: segmentDelay,
	}
}

func (s *DiscordChannelSender) SendText(ctx context.Context, target ChannelDeliveryTarget, text string) ([]string, error) {
	segments := splitDiscordMessage(text, 2000)
	if len(segments) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(segments))
	for idx, segment := range segments {
		req := discordCreateMessageRequest{Content: segment}
		if target.ReplyTo != nil && strings.TrimSpace(target.ReplyTo.MessageID) != "" {
			req.MessageReference = &discordMessageReferenceInput{MessageID: strings.TrimSpace(target.ReplyTo.MessageID)}
		}
		messageID, err := s.createMessage(ctx, strings.TrimSpace(target.Conversation.Target), req)
		if err != nil {
			return nil, err
		}
		if messageID != "" {
			ids = append(ids, messageID)
		}
		if idx < len(segments)-1 && s.segmentDelay > 0 {
			time.Sleep(s.segmentDelay)
		}
	}
	return ids, nil
}

func (s *DiscordChannelSender) createMessage(ctx context.Context, channelID string, reqBody discordCreateMessageRequest) (string, error) {
	if strings.TrimSpace(s.token) == "" {
		return "", fmt.Errorf("discord sender: token must not be empty")
	}
	if strings.TrimSpace(channelID) == "" {
		return "", fmt.Errorf("discord sender: channel id must not be empty")
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("discord sender: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		s.baseURL+"/channels/"+channelID+"/messages",
		bytes.NewReader(payload),
	)
	if err != nil {
		return "", fmt.Errorf("discord sender: new request: %w", err)
	}
	req.Header.Set("Authorization", "Bot "+s.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("discord sender: create message: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("discord sender: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var failure discordErrorResponse
		if json.Unmarshal(body, &failure) == nil && strings.TrimSpace(failure.Message) != "" {
			return "", fmt.Errorf("discord sender: create message failed: %s", strings.TrimSpace(failure.Message))
		}
		return "", fmt.Errorf("discord sender: create message failed: status %d", resp.StatusCode)
	}
	var result discordCreateMessageResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("discord sender: decode response: %w", err)
	}
	return strings.TrimSpace(result.ID), nil
}

func SplitDiscordMessage(text string, limit int) []string {
	return splitDiscordMessage(text, limit)
}

func splitDiscordMessage(text string, limit int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	runes := []rune(text)
	if limit <= 0 || len(runes) <= limit {
		return []string{text}
	}

	var segments []string
	remaining := runes
	for len(remaining) > limit {
		cut := chooseDiscordSplitPoint(remaining, limit)
		segment := strings.TrimSpace(string(remaining[:cut]))
		if segment != "" {
			segments = append(segments, segment)
		}
		remaining = []rune(strings.TrimSpace(string(remaining[cut:])))
	}
	if len(remaining) > 0 {
		segments = append(segments, string(remaining))
	}
	return segments
}

func chooseDiscordSplitPoint(text []rune, limit int) int {
	window := string(text[:limit])
	for _, marker := range []string{"\n\n", "\n", ". ", "! ", "? ", "。", "！", "？", " "} {
		if idx := strings.LastIndex(window, marker); idx > 0 {
			return utf8.RuneCountInString(window[:idx+len(marker)])
		}
	}
	return limit
}

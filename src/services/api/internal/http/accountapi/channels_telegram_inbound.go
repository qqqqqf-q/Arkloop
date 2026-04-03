package accountapi

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"arkloop/services/api/internal/data"
	"arkloop/services/shared/messagecontent"

	"github.com/google/uuid"
)

type telegramInboundAttachment struct {
	Type            string `json:"type"`
	FileID          string `json:"file_id,omitempty"`
	ThumbnailFileID string `json:"thumbnail_file_id,omitempty"`
	FileName        string `json:"file_name,omitempty"`
	MimeType        string `json:"mime_type,omitempty"`
	Size            int64  `json:"size,omitempty"`
	Width           int    `json:"width,omitempty"`
	Height          int    `json:"height,omitempty"`
	DurationMs      int64  `json:"duration_ms,omitempty"`
	Caption         string `json:"caption,omitempty"`
}

type telegramIncomingMessage struct {
	ChannelID         uuid.UUID
	ChannelType       string
	PlatformChatID    string
	PlatformMsgID     string
	PlatformUserID    string
	PlatformUsername  string
	ChatType          string
	ConversationTitle string
	DateUnix          int64
	Text              string
	CommandText       string
	MediaAttachments  []telegramInboundAttachment
	ReplyToMsgID      *string
	ReplyToPreview    string
	MentionsBot       bool
	IsReplyToBot      bool
	MessageThreadID   *string
	RawPayload        json.RawMessage
}

func (m telegramIncomingMessage) IsPrivate() bool {
	return strings.EqualFold(strings.TrimSpace(m.ChatType), "private")
}

func (m telegramIncomingMessage) HasContent() bool {
	return strings.TrimSpace(m.Text) != "" || len(m.MediaAttachments) > 0
}

func (m telegramIncomingMessage) ShouldCreateRun() bool {
	return m.IsPrivate() || m.MentionsBot || m.IsReplyToBot
}

func normalizeTelegramIncomingMessage(
	channelID uuid.UUID,
	channelType string,
	rawPayload []byte,
	update telegramUpdate,
	botUsername string,
	telegramBotUserID int64,
) (*telegramIncomingMessage, error) {
	if update.Message == nil || update.Message.From == nil {
		return nil, nil
	}
	msg := update.Message
	bodyText := strings.TrimSpace(resolveTelegramMessageBody(msg))
	attachments := collectTelegramInboundAttachments(msg)
	if strings.TrimSpace(bodyText) == "" && len(attachments) == 0 {
		return nil, nil
	}
	replyToMessageID := optionalTelegramMessageID(msg.ReplyToMessage)
	replyToPreview := buildTelegramReplyPreview(msg.ReplyToMessage)
	messageThreadID := optionalTelegramThreadID(msg.MessageThreadID)
	incoming := &telegramIncomingMessage{
		ChannelID:         channelID,
		ChannelType:       channelType,
		PlatformChatID:    strconv.FormatInt(msg.Chat.ID, 10),
		PlatformMsgID:     strconv.FormatInt(msg.MessageID, 10),
		PlatformUserID:    strconv.FormatInt(msg.From.ID, 10),
		PlatformUsername:  trimOptional(msg.From.Username),
		ChatType:          strings.TrimSpace(msg.Chat.Type),
		ConversationTitle: strings.TrimSpace(firstNonEmpty(trimOptional(msg.Chat.Title), trimOptional(msg.Chat.Username))),
		DateUnix:          msg.Date,
		Text:              bodyText,
		CommandText:       bodyText,
		MediaAttachments:  attachments,
		ReplyToMsgID:      replyToMessageID,
		ReplyToPreview:    replyToPreview,
		MentionsBot:       telegramMessageMentionsBot(msg, botUsername),
		IsReplyToBot:      telegramMessageRepliesToBot(msg, telegramBotUserID),
		MessageThreadID:   messageThreadID,
		RawPayload:        json.RawMessage(rawPayload),
	}
	return incoming, nil
}

func resolveTelegramMessageBody(msg *telegramMessage) string {
	if msg == nil {
		return ""
	}
	if strings.TrimSpace(msg.Text) != "" {
		return strings.TrimSpace(msg.Text)
	}
	return strings.TrimSpace(msg.Caption)
}

func telegramMessageMentionsBot(msg *telegramMessage, botUsername string) bool {
	if msg == nil {
		return false
	}
	text := strings.ToLower(resolveTelegramMessageBody(msg))
	cleanBotUsername := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(botUsername, "@")))
	if cleanBotUsername != "" && strings.Contains(text, "@"+cleanBotUsername) {
		return true
	}
	for _, entity := range append(append([]telegramMessageEntity{}, msg.Entities...), msg.CaptionEntities...) {
		switch strings.TrimSpace(entity.Type) {
		case "text_mention":
			if entity.User != nil && entity.User.IsBot {
				return true
			}
		case "mention":
			if cleanBotUsername == "" {
				continue
			}
			entityText := sliceTelegramEntityText(resolveTelegramMessageBody(msg), entity.Offset, entity.Length)
			if strings.EqualFold(strings.TrimSpace(entityText), "@"+cleanBotUsername) {
				return true
			}
		}
	}
	return false
}

func telegramMessageRepliesToBot(msg *telegramMessage, telegramBotUserID int64) bool {
	if msg == nil || msg.ReplyToMessage == nil || msg.ReplyToMessage.From == nil {
		return false
	}
	if telegramBotUserID != 0 {
		return msg.ReplyToMessage.From.ID == telegramBotUserID
	}
	return false
}

func collectTelegramInboundAttachments(msg *telegramMessage) []telegramInboundAttachment {
	if msg == nil {
		return nil
	}
	caption := strings.TrimSpace(msg.Caption)
	items := make([]telegramInboundAttachment, 0, 7)
	if len(msg.Photo) > 0 {
		best := msg.Photo[0]
		sort.Slice(msg.Photo, func(i, j int) bool {
			left := int64(msg.Photo[i].Width) * int64(msg.Photo[i].Height)
			right := int64(msg.Photo[j].Width) * int64(msg.Photo[j].Height)
			return left > right
		})
		best = msg.Photo[0]
		items = append(items, telegramInboundAttachment{
			Type:    "image",
			FileID:  strings.TrimSpace(best.FileID),
			Size:    best.FileSize,
			Width:   best.Width,
			Height:  best.Height,
			Caption: caption,
		})
	}
	if msg.Document != nil {
		items = append(items, telegramInboundAttachment{
			Type:     "document",
			FileID:   strings.TrimSpace(msg.Document.FileID),
			FileName: strings.TrimSpace(msg.Document.FileName),
			MimeType: strings.TrimSpace(msg.Document.MimeType),
			Size:     msg.Document.FileSize,
			Caption:  caption,
		})
	}
	if msg.Audio != nil {
		items = append(items, telegramInboundAttachment{
			Type:       "audio",
			FileID:     strings.TrimSpace(msg.Audio.FileID),
			FileName:   strings.TrimSpace(msg.Audio.FileName),
			MimeType:   strings.TrimSpace(msg.Audio.MimeType),
			Size:       msg.Audio.FileSize,
			DurationMs: int64(msg.Audio.Duration) * 1000,
			Caption:    caption,
		})
	}
	if msg.Voice != nil {
		items = append(items, telegramInboundAttachment{
			Type:       "voice",
			FileID:     strings.TrimSpace(msg.Voice.FileID),
			MimeType:   strings.TrimSpace(msg.Voice.MimeType),
			Size:       msg.Voice.FileSize,
			DurationMs: int64(msg.Voice.Duration) * 1000,
			Caption:    caption,
		})
	}
	if msg.Video != nil {
		items = append(items, telegramInboundAttachment{
			Type:       "video",
			FileID:     strings.TrimSpace(msg.Video.FileID),
			FileName:   strings.TrimSpace(msg.Video.FileName),
			MimeType:   strings.TrimSpace(msg.Video.MimeType),
			Size:       msg.Video.FileSize,
			Width:      msg.Video.Width,
			Height:     msg.Video.Height,
			DurationMs: int64(msg.Video.Duration) * 1000,
			Caption:    caption,
		})
	}
	if msg.Animation != nil {
		att := telegramInboundAttachment{
			Type:       "animation",
			FileID:     strings.TrimSpace(msg.Animation.FileID),
			FileName:   strings.TrimSpace(msg.Animation.FileName),
			MimeType:   strings.TrimSpace(msg.Animation.MimeType),
			Size:       msg.Animation.FileSize,
			Width:      msg.Animation.Width,
			Height:     msg.Animation.Height,
			DurationMs: int64(msg.Animation.Duration) * 1000,
			Caption:    caption,
		}
		if msg.Animation.Thumbnail != nil {
			att.ThumbnailFileID = strings.TrimSpace(msg.Animation.Thumbnail.FileID)
		}
		items = append(items, att)
	}
	if msg.Sticker != nil {
		att := telegramInboundAttachment{
			Type:    "sticker",
			FileID:  strings.TrimSpace(msg.Sticker.FileID),
			Size:    msg.Sticker.FileSize,
			Width:   msg.Sticker.Width,
			Height:  msg.Sticker.Height,
			Caption: caption,
		}
		if msg.Sticker.Thumbnail != nil {
			att.ThumbnailFileID = strings.TrimSpace(msg.Sticker.Thumbnail.FileID)
		}
		items = append(items, att)
	}
	return items
}

func telegramInboundDisplayName(identity data.ChannelIdentity, incoming telegramIncomingMessage) string {
	displayName := incoming.PlatformUserID
	if identity.DisplayName != nil && strings.TrimSpace(*identity.DisplayName) != "" {
		displayName = strings.TrimSpace(*identity.DisplayName)
	}
	return displayName
}

func telegramInboundMetadataJSON(identity data.ChannelIdentity, incoming telegramIncomingMessage, displayName string) (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"source":              "telegram",
		"channel_identity_id": identity.ID.String(),
		"display_name":        displayName,
		"platform_chat_id":    incoming.PlatformChatID,
		"platform_message_id": incoming.PlatformMsgID,
		"platform_user_id":    incoming.PlatformUserID,
		"platform_username":   incoming.PlatformUsername,
		"chat_type":           incoming.ChatType,
		"conversation_title":  incoming.ConversationTitle,
		"mentions_bot":        incoming.MentionsBot,
		"is_reply_to_bot":     incoming.IsReplyToBot,
		"media_attachments":   incoming.MediaAttachments,
		"reply_to_message_id": incoming.ReplyToMsgID,
		"message_thread_id":   incoming.MessageThreadID,
	})
}

func buildTelegramStructuredMessage(
	identity data.ChannelIdentity,
	incoming telegramIncomingMessage,
) (string, json.RawMessage, json.RawMessage, error) {
	prefix := "[Telegram]"
	if !incoming.IsPrivate() && strings.TrimSpace(incoming.ConversationTitle) != "" {
		prefix = "[Telegram in " + strings.TrimSpace(incoming.ConversationTitle) + "]"
	}
	body := prefix + " " + strings.TrimSpace(incoming.Text)
	attachmentBlock := renderTelegramAttachmentBlock(incoming.MediaAttachments)
	if attachmentBlock != "" {
		if body != "" {
			body += "\n\n" + attachmentBlock
		} else {
			body = attachmentBlock
		}
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return "", nil, nil, fmt.Errorf("telegram inbound message content is empty")
	}
	displayName := telegramInboundDisplayName(identity, incoming)
	projection := buildTelegramEnvelopeText(identity.ID, incoming, displayName, body)
	content, err := messagecontent.Normalize(messagecontent.FromText(projection).Parts)
	if err != nil {
		return "", nil, nil, err
	}
	contentJSON, err := content.JSON()
	if err != nil {
		return "", nil, nil, err
	}
	metadataJSON, err := telegramInboundMetadataJSON(identity, incoming, displayName)
	if err != nil {
		return "", nil, nil, err
	}
	return projection, contentJSON, metadataJSON, nil
}

func buildTelegramEnvelopeText(identityID uuid.UUID, incoming telegramIncomingMessage, displayName, body string) string {
	lines := []string{
		fmt.Sprintf(`display-name: "%s"`, escapeTelegramEnvelopeValue(displayName)),
		`channel: "telegram"`,
		fmt.Sprintf(`conversation-type: "%s"`, escapeTelegramEnvelopeValue(normalizeTelegramConversationType(incoming.ChatType))),
	}
	if identityID != uuid.Nil {
		lines = append(lines, fmt.Sprintf(`sender-ref: "%s"`, identityID.String()))
	}
	if strings.TrimSpace(incoming.PlatformUsername) != "" {
		lines = append(lines, fmt.Sprintf(`platform-username: "%s"`, escapeTelegramEnvelopeValue(incoming.PlatformUsername)))
	}
	if title := strings.TrimSpace(incoming.ConversationTitle); title != "" {
		lines = append(lines, fmt.Sprintf(`conversation-title: "%s"`, escapeTelegramEnvelopeValue(title)))
	}
	if incoming.ReplyToMsgID != nil && strings.TrimSpace(*incoming.ReplyToMsgID) != "" {
		lines = append(lines, fmt.Sprintf(`reply-to-message-id: "%s"`, escapeTelegramEnvelopeValue(strings.TrimSpace(*incoming.ReplyToMsgID))))
		if strings.TrimSpace(incoming.ReplyToPreview) != "" {
			lines = append(lines, fmt.Sprintf(`reply-to-preview: "%s"`, escapeTelegramEnvelopeValue(incoming.ReplyToPreview)))
		}
	}
	if incoming.MessageThreadID != nil && strings.TrimSpace(*incoming.MessageThreadID) != "" {
		lines = append(lines, fmt.Sprintf(`message-thread-id: "%s"`, escapeTelegramEnvelopeValue(strings.TrimSpace(*incoming.MessageThreadID))))
	}
	lines = append(lines, fmt.Sprintf(`time: "%s"`, escapeTelegramEnvelopeValue(formatTelegramTimestamp(incoming.DateUnix))))
	return "---\n" + strings.Join(lines, "\n") + "\n---\n" + body
}

func normalizeTelegramConversationType(chatType string) string {
	cleaned := strings.TrimSpace(chatType)
	if cleaned == "" {
		return "private"
	}
	return cleaned
}

func renderTelegramAttachmentBlock(items []telegramInboundAttachment) string {
	if len(items) == 0 {
		return ""
	}
	lines := make([]string, 0, len(items))
	for _, item := range items {
		label := strings.TrimSpace(item.FileName)
		if label == "" {
			label = strings.TrimSpace(item.Type)
		}
		if label == "" {
			label = "attachment"
		}
		lines = append(lines, fmt.Sprintf("[%s: %s]", telegramAttachmentLabel(item.Type), label))
	}
	return strings.Join(lines, "\n")
}

func telegramAttachmentLabel(kind string) string {
	switch strings.TrimSpace(kind) {
	case "image":
		return "图片"
	case "document":
		return "附件"
	case "audio":
		return "音频"
	case "voice":
		return "语音"
	case "video":
		return "视频"
	case "animation":
		return "动画"
	case "sticker":
		return "贴纸"
	default:
		return "附件"
	}
}

func optionalTelegramMessageID(msg *telegramMessage) *string {
	if msg == nil || msg.MessageID == 0 {
		return nil
	}
	value := strconv.FormatInt(msg.MessageID, 10)
	return &value
}

const telegramReplyPreviewMaxRunes = 80

func buildTelegramReplyPreview(msg *telegramMessage) string {
	if msg == nil {
		return ""
	}
	senderName := ""
	if msg.From != nil {
		parts := []string{trimOptional(msg.From.FirstName), trimOptional(msg.From.LastName)}
		senderName = strings.TrimSpace(strings.Join(parts, " "))
		if senderName == "" {
			senderName = trimOptional(msg.From.Username)
		}
	}
	text := strings.TrimSpace(resolveTelegramMessageBody(msg))
	if text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) > telegramReplyPreviewMaxRunes {
		text = string(runes[:telegramReplyPreviewMaxRunes]) + "..."
	}
	// 折叠换行，模拟 Telegram 客户端的单行预览
	text = strings.Join(strings.Fields(text), " ")
	if senderName != "" {
		return senderName + ": " + text
	}
	return text
}

func optionalTelegramThreadID(threadID *int64) *string {
	if threadID == nil || *threadID == 0 {
		return nil
	}
	value := strconv.FormatInt(*threadID, 10)
	return &value
}

func sliceTelegramEntityText(text string, offset int, length int) string {
	runes := []rune(text)
	if offset < 0 || length <= 0 || offset >= len(runes) || offset+length > len(runes) {
		return ""
	}
	return string(runes[offset : offset+length])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func escapeTelegramEnvelopeValue(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return replacer.Replace(strings.TrimSpace(value))
}

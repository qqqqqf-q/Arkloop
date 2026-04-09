package accountapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/http/conversationapi"
	"arkloop/services/shared/messagecontent"
	"arkloop/services/shared/objectstore"
	"arkloop/services/shared/telegrambot"

	"github.com/google/uuid"
)

// MessageAttachmentPutStore Telegram 入站媒体写入对象存储所需的最小接口。
type MessageAttachmentPutStore interface {
	PutObject(ctx context.Context, key string, data []byte, options objectstore.PutOptions) error
}

func shouldIngestTelegramAttachment(att telegramInboundAttachment) bool {
	switch strings.TrimSpace(att.Type) {
	case "image":
		return strings.TrimSpace(att.FileID) != ""
	case "sticker":
		return strings.TrimSpace(att.FileID) != "" || strings.TrimSpace(att.ThumbnailFileID) != ""
	case "animation":
		return strings.TrimSpace(att.ThumbnailFileID) != ""
	case "document":
		mt := strings.ToLower(strings.TrimSpace(att.MimeType))
		return strings.HasPrefix(mt, "image/") && strings.TrimSpace(att.FileID) != ""
	default:
		return false
	}
}

func telegramAttachmentDeclaredMime(att telegramInboundAttachment, sniffed string) string {
	if m := strings.TrimSpace(att.MimeType); m != "" {
		return m
	}
	return sniffed
}

func defaultFilenameForTelegramAttachment(att telegramInboundAttachment, mime string) string {
	m := strings.ToLower(strings.TrimSpace(mime))
	switch strings.TrimSpace(att.Type) {
	case "sticker":
		if strings.Contains(m, "webp") {
			return "sticker.webp"
		}
		if strings.Contains(m, "png") {
			return "sticker.png"
		}
		return "sticker.jpg"
	case "animation":
		switch {
		case strings.Contains(m, "png"):
			return "animation.png"
		case strings.Contains(m, "webp"):
			return "animation.webp"
		default:
			return "animation.jpg"
		}
	case "image":
		switch {
		case strings.Contains(m, "png"):
			return "image.png"
		case strings.Contains(m, "gif"):
			return "image.gif"
		case strings.Contains(m, "webp"):
			return "image.webp"
		default:
			return "image.jpg"
		}
	default:
		if n := strings.TrimSpace(att.FileName); n != "" {
			return conversationapi.SanitizeAttachmentFilename(n)
		}
		return "image.jpg"
	}
}

func resolveIngestFileID(att telegramInboundAttachment) string {
	switch strings.TrimSpace(att.Type) {
	case "animation":
		return strings.TrimSpace(att.ThumbnailFileID)
	case "sticker":
		if strings.TrimSpace(att.FileID) != "" {
			return strings.TrimSpace(att.FileID)
		}
		return strings.TrimSpace(att.ThumbnailFileID)
	default:
		return strings.TrimSpace(att.FileID)
	}
}

func ingestTelegramMediaAttachments(
	ctx context.Context,
	client *telegrambot.Client,
	store MessageAttachmentPutStore,
	token string,
	accountID, threadID uuid.UUID,
	userID *uuid.UUID,
	items []telegramInboundAttachment,
) (ingested []messagecontent.Part, remaining []telegramInboundAttachment, err error) {
	for _, att := range items {
		if !shouldIngestTelegramAttachment(att) {
			remaining = append(remaining, att)
			continue
		}
		fileID := resolveIngestFileID(att)
		if fileID == "" {
			remaining = append(remaining, att)
			continue
		}
		tf, gerr := client.GetFile(ctx, token, fileID)
		if gerr != nil {
			return nil, nil, gerr
		}
		data, sniffed, derr := client.DownloadBotFile(ctx, token, tf.FilePath, conversationapi.MaxImageAttachmentBytes)
		if derr != nil {
			return nil, nil, derr
		}
		declared := telegramAttachmentDeclaredMime(att, sniffed)
		if strings.TrimSpace(att.Type) == "animation" || strings.TrimSpace(att.Type) == "sticker" {
			// thumbnail 总是 JPEG
			declared = sniffed
		}
		displayFilename := strings.TrimSpace(conversationapi.SanitizeAttachmentFilename(att.FileName))
		if displayFilename == "" {
			displayFilename = defaultFilenameForTelegramAttachment(att, declared)
		} else if !strings.Contains(displayFilename, ".") {
			displayFilename += extForImageMIME(declared)
		}
		payload, perr := conversationapi.BuildAttachmentUploadPayload(displayFilename, declared, data)
		if perr != nil {
			if strings.TrimSpace(att.Type) == "sticker" && strings.TrimSpace(att.ThumbnailFileID) != "" && fileID != strings.TrimSpace(att.ThumbnailFileID) {
				// sticker 原始文件 MIME 不被支持 -> 回退 thumbnail
				thumbTF, tgerr := client.GetFile(ctx, token, strings.TrimSpace(att.ThumbnailFileID))
				if tgerr != nil {
					return nil, nil, tgerr
				}
				data, sniffed, derr = client.DownloadBotFile(ctx, token, thumbTF.FilePath, conversationapi.MaxImageAttachmentBytes)
				if derr != nil {
					return nil, nil, derr
				}
				declared = sniffed
				displayFilename = defaultFilenameForTelegramAttachment(att, declared)
				payload, perr = conversationapi.BuildAttachmentUploadPayload(displayFilename, declared, data)
				if perr != nil {
					return nil, nil, perr
				}
			} else if strings.TrimSpace(att.Type) == "sticker" || strings.TrimSpace(att.Type) == "animation" {
				remaining = append(remaining, att)
				continue
			} else {
				return nil, nil, perr
			}
		}
		keySuffix := conversationapi.SanitizeAttachmentKeyName(displayFilename)
		key := fmt.Sprintf("attachments/%s/%s/%s", accountID.String(), uuid.NewString(), keySuffix)
		threadIDText := threadID.String()
		ownerID := ""
		if userID != nil {
			ownerID = userID.String()
		}
		meta := objectstore.ArtifactMetadata(
			conversationapi.MessageAttachmentOwnerKind,
			ownerID,
			accountID.String(),
			&threadIDText,
		)
		if perr := store.PutObject(ctx, key, payload.Bytes, objectstore.PutOptions{ContentType: payload.MimeType, Metadata: meta}); perr != nil {
			return nil, nil, perr
		}
		ref := &messagecontent.AttachmentRef{
			Key:      key,
			Filename: displayFilename,
			MimeType: payload.MimeType,
			Size:     int64(len(payload.Bytes)),
		}
		switch payload.Kind {
		case messagecontent.PartTypeImage:
			ingested = append(ingested, messagecontent.Part{Type: messagecontent.PartTypeImage, Attachment: ref})
		case messagecontent.PartTypeFile:
			ingested = append(ingested, messagecontent.Part{
				Type:          messagecontent.PartTypeFile,
				Attachment:    ref,
				ExtractedText: payload.ExtractedText,
			})
		default:
			return nil, nil, fmt.Errorf("telegram inbound: unexpected attachment kind %q", payload.Kind)
		}
	}
	return ingested, remaining, nil
}

func extForImageMIME(mime string) string {
	m := strings.ToLower(strings.TrimSpace(strings.Split(mime, ";")[0]))
	switch m {
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ".jpg"
	}
}

func buildTelegramStructuredMessageWithMedia(
	ctx context.Context,
	client *telegrambot.Client,
	store MessageAttachmentPutStore,
	token string,
	accountID, threadID uuid.UUID,
	userID *uuid.UUID,
	identity data.ChannelIdentity,
	incoming telegramIncomingMessage,
	timeCtx inboundTimeContext,
) (string, json.RawMessage, json.RawMessage, error) {
	displayName := telegramInboundDisplayName(identity, incoming)
	if store == nil || client == nil || strings.TrimSpace(token) == "" {
		return buildTelegramStructuredMessage(identity, incoming, timeCtx)
	}

	mediaParts, remaining, err := ingestTelegramMediaAttachments(ctx, client, store, token, accountID, threadID, userID, incoming.MediaAttachments)
	if err != nil {
		return "", nil, nil, err
	}

	userPrefix := "[Telegram]"
	if !incoming.IsPrivate() && strings.TrimSpace(incoming.ConversationTitle) != "" {
		userPrefix = "[Telegram in " + strings.TrimSpace(incoming.ConversationTitle) + "]"
	}
	userBody := userPrefix + " " + strings.TrimSpace(incoming.Text)
	attachBlock := renderTelegramAttachmentBlock(remaining)
	if attachBlock != "" {
		if userBody != "" {
			userBody += "\n\n" + attachBlock
		} else {
			userBody = attachBlock
		}
	}
	userBody = strings.TrimSpace(userBody)
	if userBody == "" && len(mediaParts) == 0 {
		return "", nil, nil, fmt.Errorf("telegram inbound message content is empty")
	}

	envelope := buildTelegramEnvelopeText(identity.ID, incoming, displayName, userBody, timeCtx)
	parts := []messagecontent.Part{{Type: messagecontent.PartTypeText, Text: envelope}}
	parts = append(parts, mediaParts...)

	content, err := messagecontent.Normalize(parts)
	if err != nil {
		return "", nil, nil, err
	}

	_, projection, raw, err := conversationapi.FinalizeMessageContent(content)
	if err != nil {
		return "", nil, nil, err
	}
	metadataJSON, err := telegramInboundMetadataJSON(identity, incoming, displayName, timeCtx)
	if err != nil {
		return "", nil, nil, err
	}
	return projection, raw, metadataJSON, nil
}

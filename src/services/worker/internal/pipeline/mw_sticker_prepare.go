package pipeline

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"arkloop/services/shared/messagecontent"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/llm"

	"github.com/google/uuid"
)

func NewStickerPrepareMiddleware(db data.DB, store MessageAttachmentStore) RunMiddleware {
	repo := data.AccountStickersRepository{}
	cacheRepo := data.StickerDescriptionCacheRepository{}

	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if rc == nil || !isStickerRegisterRun(rc) {
			return next(ctx, rc)
		}
		rc.StickerRegisterRun = true
		rc.AllowlistSet = map[string]struct{}{}

		stickerID := strings.TrimSpace(stringValue(rc.InputJSON["sticker_id"]))
		if stickerID == "" || db == nil || store == nil {
			return nil
		}
		sticker, err := repo.GetByHash(ctx, db, rc.Run.AccountID, stickerID)
		if err != nil {
			return err
		}
		if sticker == nil || sticker.IsRegistered {
			return nil
		}
		if strings.TrimSpace(sticker.PreviewStorageKey) == "" || !supportsImageInput(rc.SelectedRoute) {
			return nil
		}

		imageBytes, contentType, err := store.GetWithContentType(ctx, sticker.PreviewStorageKey)
		if err != nil {
			return fmt.Errorf("load sticker preview %s: %w", stickerID, err)
		}
		if len(imageBytes) == 0 {
			return nil
		}

		rc.Messages = append(rc.Messages, llm.Message{
			Role: "user",
			Content: []llm.ContentPart{
				{
					Type: "text",
					Text: "请分析这张 Telegram sticker 预览图，并严格按以下两行格式输出：\n描述: <100字内描述>\n标签: <1-3个逗号分隔短标签>",
				},
				{
					Type: messagecontent.PartTypeImage,
					Data: imageBytes,
					Attachment: &messagecontent.AttachmentRef{
						Key:      sticker.PreviewStorageKey,
						Filename: filepath.Base(sticker.PreviewStorageKey),
						MimeType: contentType,
						Size:     int64(len(imageBytes)),
					},
				},
			},
		})
		rc.ThreadMessageIDs = append(rc.ThreadMessageIDs, uuid.Nil)

		err = next(ctx, rc)
		if err != nil {
			return err
		}

		description, tags, ok := parseStickerBuilderOutput(rc.FinalAssistantOutput)
		if !ok {
			return nil
		}
		if err := cacheRepo.Upsert(ctx, db, stickerID, description, tags); err != nil {
			return fmt.Errorf("cache sticker description %s: %w", stickerID, err)
		}
		if err := repo.MarkRegistered(ctx, db, rc.Run.AccountID, stickerID, description, tags); err != nil {
			return fmt.Errorf("mark sticker registered %s: %w", stickerID, err)
		}
		return nil
	}
}

func parseStickerBuilderOutput(raw string) (description string, tags string, ok bool) {
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "描述:"):
			description = strings.TrimSpace(strings.TrimPrefix(trimmed, "描述:"))
		case strings.HasPrefix(trimmed, "标签:"):
			tags = normalizeStickerTags(strings.TrimSpace(strings.TrimPrefix(trimmed, "标签:")))
		}
	}
	if description == "" || tags == "" {
		return "", "", false
	}
	return description, tags, true
}

func normalizeStickerTags(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '，'
	})
	seen := map[string]struct{}{}
	out := make([]string, 0, 3)
	for _, part := range parts {
		tag := strings.TrimSpace(part)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
		if len(out) == 3 {
			break
		}
	}
	return strings.Join(out, ", ")
}

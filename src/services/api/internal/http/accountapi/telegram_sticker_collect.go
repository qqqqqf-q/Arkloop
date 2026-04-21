package accountapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"path/filepath"
	"strings"
	"time"

	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/http/conversationapi"
	"arkloop/services/shared/objectstore"
	"arkloop/services/shared/runkind"
	"arkloop/services/shared/telegrambot"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type telegramCollectedSticker struct {
	ContentHash       string
	StorageKey        string
	PreviewStorageKey string
	FileSize          int64
	MimeType          string
	IsAnimated        bool
}

func (c telegramConnector) maybeCollectTelegramStickersTx(
	ctx context.Context,
	tx pgx.Tx,
	ch data.Channel,
	identityID *uuid.UUID,
	items []telegramCollectedSticker,
) error {
	if len(items) == 0 || tx == nil || c.projectRepo == nil || c.threadRepo == nil || c.runEventRepo == nil || c.jobRepo == nil {
		return nil
	}

	repo, err := data.NewAccountStickersRepository(tx)
	if err != nil {
		return err
	}
	cacheRepo, err := data.NewStickerDescriptionCacheRepository(tx)
	if err != nil {
		return err
	}

	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		contentHash := strings.TrimSpace(item.ContentHash)
		if contentHash == "" {
			continue
		}
		if _, ok := seen[contentHash]; ok {
			continue
		}
		seen[contentHash] = struct{}{}

		sticker, shouldRegister, err := upsertTelegramStickerPendingTx(ctx, tx, ch.AccountID, item)
		if err != nil {
			return err
		}
		cache, err := cacheRepo.Get(ctx, contentHash)
		if err != nil {
			return err
		}
		if cache != nil && strings.TrimSpace(cache.Description) != "" {
			if err := repo.MarkRegistered(ctx, ch.AccountID, contentHash, cache.Description, cache.EmotionTags); err != nil {
				return err
			}
			continue
		}
		if sticker == nil || sticker.IsRegistered {
			continue
		}
		if strings.TrimSpace(sticker.PreviewStorageKey) == "" || !shouldRegister {
			continue
		}
		if err := c.enqueueTelegramStickerRegisterRunTx(ctx, tx, ch, identityID, contentHash); err != nil {
			return err
		}
	}
	return nil
}

func shouldTriggerStickerRegister(existing *data.AccountSticker) bool {
	if existing == nil {
		return true
	}
	if existing.IsRegistered {
		return false
	}
	return time.Since(existing.UpdatedAt) >= time.Hour
}

func upsertTelegramStickerPendingTx(
	ctx context.Context,
	tx pgx.Tx,
	accountID uuid.UUID,
	item telegramCollectedSticker,
) (*data.AccountSticker, bool, error) {
	contentHash := strings.TrimSpace(item.ContentHash)
	if tx == nil || accountID == uuid.Nil || contentHash == "" {
		return nil, false, nil
	}

	existing, err := loadTelegramStickerForUpdateTx(ctx, tx, accountID, contentHash)
	if err != nil {
		return nil, false, err
	}
	if existing == nil {
		createdAt := time.Now().UTC()
		sticker := &data.AccountSticker{
			ID:                uuid.New(),
			AccountID:         accountID,
			ContentHash:       contentHash,
			StorageKey:        strings.TrimSpace(item.StorageKey),
			PreviewStorageKey: strings.TrimSpace(item.PreviewStorageKey),
			FileSize:          item.FileSize,
			MimeType:          strings.TrimSpace(item.MimeType),
			IsAnimated:        item.IsAnimated,
			CreatedAt:         createdAt,
			UpdatedAt:         createdAt,
		}
		if err := insertTelegramStickerPendingTx(ctx, tx, *sticker); err != nil {
			return nil, false, err
		}
		return sticker, true, nil
	}

	shouldRegister := shouldTriggerStickerRegister(existing)
	hadPreview := strings.TrimSpace(existing.PreviewStorageKey) != ""
	incomingHasPreview := strings.TrimSpace(item.PreviewStorageKey) != ""
	if !shouldRegister && !existing.IsRegistered && !hadPreview && incomingHasPreview {
		shouldRegister = true
	}
	existing.StorageKey = strings.TrimSpace(item.StorageKey)
	existing.PreviewStorageKey = strings.TrimSpace(item.PreviewStorageKey)
	existing.FileSize = item.FileSize
	existing.MimeType = strings.TrimSpace(item.MimeType)
	existing.IsAnimated = item.IsAnimated
	if shouldRegister && !existing.IsRegistered {
		existing.UpdatedAt = time.Now().UTC()
	}
	if err := updateTelegramStickerPendingTx(ctx, tx, *existing, shouldRegister && !existing.IsRegistered); err != nil {
		return nil, false, err
	}
	return existing, shouldRegister, nil
}

func loadTelegramStickerForUpdateTx(
	ctx context.Context,
	tx pgx.Tx,
	accountID uuid.UUID,
	contentHash string,
) (*data.AccountSticker, error) {
	var item data.AccountSticker
	err := tx.QueryRow(ctx, `
		SELECT id, account_id, content_hash, storage_key, preview_storage_key, file_size, mime_type,
		       is_animated, short_tags, long_desc, usage_count, last_used_at, is_registered, created_at, updated_at
		  FROM account_stickers
		 WHERE account_id = $1
		   AND content_hash = $2
		 FOR UPDATE`,
		accountID, strings.TrimSpace(contentHash),
	).Scan(
		&item.ID,
		&item.AccountID,
		&item.ContentHash,
		&item.StorageKey,
		&item.PreviewStorageKey,
		&item.FileSize,
		&item.MimeType,
		&item.IsAnimated,
		&item.ShortTags,
		&item.LongDesc,
		&item.UsageCount,
		&item.LastUsedAt,
		&item.IsRegistered,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func insertTelegramStickerPendingTx(ctx context.Context, tx pgx.Tx, item data.AccountSticker) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO account_stickers (
			id, account_id, content_hash, storage_key, preview_storage_key, file_size, mime_type,
			is_animated, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10
		)`,
		item.ID,
		item.AccountID,
		strings.TrimSpace(item.ContentHash),
		strings.TrimSpace(item.StorageKey),
		strings.TrimSpace(item.PreviewStorageKey),
		item.FileSize,
		strings.TrimSpace(item.MimeType),
		item.IsAnimated,
		item.CreatedAt.UTC(),
		item.UpdatedAt.UTC(),
	)
	return err
}

func updateTelegramStickerPendingTx(
	ctx context.Context,
	tx pgx.Tx,
	item data.AccountSticker,
	touchUpdatedAt bool,
) error {
	query := `
		UPDATE account_stickers
		   SET storage_key = $3,
		       preview_storage_key = $4,
		       file_size = $5,
		       mime_type = $6,
		       is_animated = $7
		 WHERE account_id = $1
		   AND content_hash = $2`
	args := []any{
		item.AccountID,
		strings.TrimSpace(item.ContentHash),
		strings.TrimSpace(item.StorageKey),
		strings.TrimSpace(item.PreviewStorageKey),
		item.FileSize,
		strings.TrimSpace(item.MimeType),
		item.IsAnimated,
	}
	if touchUpdatedAt {
		query = `
			UPDATE account_stickers
			   SET storage_key = $3,
			       preview_storage_key = $4,
			       file_size = $5,
			       mime_type = $6,
			       is_animated = $7,
			       updated_at = $8
			 WHERE account_id = $1
			   AND content_hash = $2`
		args = append(args, item.UpdatedAt.UTC())
	}
	_, err := tx.Exec(ctx, query, args...)
	return err
}

func (c telegramConnector) enqueueTelegramStickerRegisterRunTx(
	ctx context.Context,
	tx pgx.Tx,
	ch data.Channel,
	identityID *uuid.UUID,
	contentHash string,
) error {
	if ch.OwnerUserID == nil || *ch.OwnerUserID == uuid.Nil {
		return nil
	}
	startedData, err := c.buildTelegramStickerRegisterStartedData(ctx, tx, ch, identityID, contentHash)
	if err != nil {
		return err
	}
	project, err := c.projectRepo.WithTx(tx).GetOrCreateDefaultByOwner(ctx, ch.AccountID, *ch.OwnerUserID)
	if err != nil {
		return err
	}
	thread, err := c.threadRepo.WithTx(tx).Create(ctx, ch.AccountID, ch.OwnerUserID, project.ID, nil, true)
	if err != nil {
		return err
	}

	run, _, err := c.runEventRepo.WithTx(tx).CreateRunWithStartedEvent(
		ctx,
		ch.AccountID,
		thread.ID,
		ch.OwnerUserID,
		"run.started",
		startedData,
	)
	if err != nil {
		return err
	}
	_, err = c.jobRepo.WithTx(tx).EnqueueRun(
		ctx,
		ch.AccountID,
		run.ID,
		uuid.NewString(),
		data.RunExecuteJobType,
		map[string]any{
			"source":   "telegram_sticker_collect",
			"run_kind": runkind.StickerRegister,
		},
		nil,
	)
	return err
}

func (c telegramConnector) buildTelegramStickerRegisterStartedData(
	ctx context.Context,
	tx pgx.Tx,
	ch data.Channel,
	identityID *uuid.UUID,
	contentHash string,
) (map[string]any, error) {
	startedData := map[string]any{
		"run_kind":   runkind.StickerRegister,
		"persona_id": "sticker-builder",
		"sticker_id": strings.TrimSpace(contentHash),
	}
	selector, err := c.resolveTelegramStickerModelSelector(ctx, tx, ch, identityID)
	if err != nil {
		return nil, err
	}
	if selector == "" {
		return startedData, nil
	}
	startedData["model"] = selector
	allowUserScoped, err := resolveTelegramByokEnabled(ctx, c.entitlementSvc, ch.AccountID)
	if err != nil {
		return nil, err
	}
	routeID, err := resolveTelegramRouteIDBySelector(ctx, tx, ch.AccountID, selector, allowUserScoped)
	if err != nil {
		return nil, err
	}
	if routeID != "" {
		startedData["route_id"] = routeID
	}
	return startedData, nil
}

func (c telegramConnector) resolveTelegramStickerModelSelector(
	ctx context.Context,
	tx pgx.Tx,
	ch data.Channel,
	identityID *uuid.UUID,
) (string, error) {
	cfg, err := resolveTelegramConfig(ch.ChannelType, ch.ConfigJSON)
	if err != nil {
		return "", err
	}
	selector := strings.TrimSpace(cfg.DefaultModel)
	if identityID == nil || *identityID == uuid.Nil || c.channelIdentitiesRepo == nil {
		return selector, nil
	}
	preferredModel, _, err := c.channelIdentitiesRepo.WithTx(tx).GetPreferenceConfig(ctx, *identityID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(preferredModel) != "" {
		selector = strings.TrimSpace(preferredModel)
	}
	return selector, nil
}

func telegramStickerObjectKey(accountID uuid.UUID, contentHash, mimeType string, preview bool) string {
	prefix := "stickers"
	if preview {
		prefix = "sticker-previews"
	}
	ext := strings.TrimSpace(extForStickerMime(mimeType))
	if ext == "" {
		ext = ".bin"
	}
	if len(contentHash) < 2 {
		return fmt.Sprintf("%s/%s/%s%s", accountID.String(), prefix, contentHash, ext)
	}
	return fmt.Sprintf("%s/%s/%s/%s%s", accountID.String(), prefix, contentHash[:2], contentHash, ext)
}

func extForStickerMime(mimeType string) string {
	cleaned := strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
	switch cleaned {
	case "image/webp":
		return ".webp"
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "video/webm":
		return ".webm"
	case "application/x-tgsticker":
		return ".tgs"
	}
	if exts, err := mime.ExtensionsByType(cleaned); err == nil && len(exts) > 0 {
		return exts[0]
	}
	return ""
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func stickerMimeFromPath(path, fallback string) string {
	ext := strings.ToLower(strings.TrimSpace(filepath.Ext(path)))
	switch ext {
	case ".webp":
		return "image/webp"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webm":
		return "video/webm"
	case ".tgs":
		return "application/x-tgsticker"
	default:
		return strings.TrimSpace(fallback)
	}
}

func storeTelegramStickerObject(
	ctx context.Context,
	store MessageAttachmentPutStore,
	key string,
	data []byte,
	mimeType string,
	accountID uuid.UUID,
	threadID uuid.UUID,
	userID *uuid.UUID,
) error {
	if store == nil || len(data) == 0 || strings.TrimSpace(key) == "" {
		return fmt.Errorf("sticker store unavailable")
	}
	threadIDText := threadID.String()
	ownerID := ""
	if userID != nil {
		ownerID = userID.String()
	}
	return store.PutObject(ctx, key, data, objectstore.PutOptions{
		ContentType: strings.TrimSpace(mimeType),
		Metadata: objectstore.ArtifactMetadata(
			conversationapi.MessageAttachmentOwnerKind,
			ownerID,
			accountID.String(),
			&threadIDText,
		),
	})
}

func collectTelegramSticker(
	ctx context.Context,
	client *telegrambot.Client,
	store MessageAttachmentPutStore,
	token string,
	accountID, threadID uuid.UUID,
	userID *uuid.UUID,
	att telegramInboundAttachment,
	originalPath string,
	originalData []byte,
	originalMime string,
) (telegramCollectedSticker, bool) {
	if client == nil || store == nil || len(originalData) == 0 {
		return telegramCollectedSticker{}, false
	}

	contentHash := sha256Hex(originalData)
	originalMime = stickerMimeFromPath(originalPath, originalMime)
	storageKey := telegramStickerObjectKey(accountID, contentHash, originalMime, false)
	if err := storeTelegramStickerObject(ctx, store, storageKey, originalData, originalMime, accountID, threadID, userID); err != nil {
		return telegramCollectedSticker{}, false
	}

	previewData := originalData
	previewMime := originalMime
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(originalMime)), "image/") {
		previewData = nil
		previewMime = ""
	}
	if len(previewData) == 0 && strings.TrimSpace(att.ThumbnailFileID) != "" {
		thumbTF, err := client.GetFile(ctx, token, strings.TrimSpace(att.ThumbnailFileID))
		if err == nil {
			data, sniffed, derr := client.DownloadBotFile(ctx, token, thumbTF.FilePath, conversationapi.MaxImageAttachmentBytes)
			if derr == nil && len(data) > 0 {
				previewData = data
				previewMime = stickerMimeFromPath(thumbTF.FilePath, sniffed)
			}
		}
	}
	previewKey := ""
	if len(previewData) > 0 {
		previewKey = telegramStickerObjectKey(accountID, contentHash, previewMime, true)
		if err := storeTelegramStickerObject(ctx, store, previewKey, previewData, previewMime, accountID, threadID, userID); err != nil {
			previewKey = ""
		}
	}

	return telegramCollectedSticker{
		ContentHash:       contentHash,
		StorageKey:        storageKey,
		PreviewStorageKey: previewKey,
		FileSize:          int64(len(originalData)),
		MimeType:          strings.TrimSpace(originalMime),
		IsAnimated:        isAnimatedStickerMime(originalMime),
	}, true
}

func isAnimatedStickerMime(mimeType string) bool {
	cleaned := strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
	return cleaned == "video/webm" || cleaned == "application/x-tgsticker"
}

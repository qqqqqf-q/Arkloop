package pipeline

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"arkloop/services/shared/messagecontent"
	"arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/routing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type StickerPrepareConfig struct {
	AuxGateway          llm.Gateway
	EmitDebugEvents     bool
	RoutingConfigLoader *routing.ConfigLoader
	EventsRepo          CompactRunEventAppender
}

func NewStickerPrepareMiddleware(db data.DB, store MessageAttachmentStore, cfg StickerPrepareConfig) RunMiddleware {
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
		if strings.TrimSpace(sticker.PreviewStorageKey) == "" {
			return nil
		}
		if !routeSupportsVision(rc.SelectedRoute) {
			if resolution, ok := resolveStickerVisionRoute(ctx, db, rc, cfg); ok {
				estimator := stickerProviderRequestEstimator(ctx, resolution.Selected, cfg, rc.LlmMaxResponseBytes)
				swapRunContextRoute(rc, resolution, estimator)
			} else {
				return failStickerRegisterRun(ctx, rc, cfg.EventsRepo, stickerID)
			}
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


func resolveStickerVisionRoute(
	ctx context.Context,
	db data.DB,
	rc *RunContext,
	cfg StickerPrepareConfig,
) (*entitlementRouteResolution, bool) {
	// 1. spawn.profile.vision
	if resolution, ok := resolveVisionRoute(
		ctx,
		db,
		rc.Run.AccountID,
		rc.AgentConfig.ImageModel,
		cfg.AuxGateway,
		cfg.EmitDebugEvents,
		rc.LlmMaxResponseBytes,
		cfg.RoutingConfigLoader,
		rc.RoutingByokEnabled,
	); ok && routeSupportsVision(resolution.Selected) {
		return resolution, true
	}
	// 2. spawn.profile.tool (if supports vision)
	if resolution, ok := resolveEntitlementRoute(
		ctx,
		db,
		rc.Run.AccountID,
		"spawn.profile.tool",
		cfg.AuxGateway,
		cfg.EmitDebugEvents,
		rc.LlmMaxResponseBytes,
		cfg.RoutingConfigLoader,
		rc.RoutingByokEnabled,
	); ok && routeSupportsVision(resolution.Selected) {
		return resolution, true
	}
	return nil, false
}

func stickerProviderRequestEstimator(
	ctx context.Context,
	selected *routing.SelectedProviderRoute,
	cfg StickerPrepareConfig,
	llmMaxResponseBytes int,
) func(llm.Request) (int, error) {
	if selected == nil || selected.Credential.ProviderKind == routing.ProviderKindStub {
		return nil
	}
	resolved, err := ResolveGatewayConfigFromSelectedRouteForRequest(ctx, *selected, cfg.EmitDebugEvents, llmMaxResponseBytes)
	if err != nil {
		return nil
	}
	return func(req llm.Request) (int, error) {
		return llm.EstimateProviderPayloadBytes(resolved, req)
	}
}

func failStickerRegisterRun(
	ctx context.Context,
	rc *RunContext,
	eventsRepo CompactRunEventAppender,
	stickerID string,
) error {
	const (
		errorClass = llm.ErrorClassConfigMissing
		code       = "sticker.vision_model_unavailable"
		message    = "sticker vision model is not configured"
	)
	details := map[string]any{
		"sticker_id": strings.TrimSpace(stickerID),
	}
	if rc != nil && rc.SelectedRoute != nil {
		details["selected_model"] = rc.SelectedRoute.Route.Model
		details["selected_route_id"] = rc.SelectedRoute.Route.ID
	}
	if eventsRepo == nil || rc == nil || rc.DB == nil || rc.RunStatusDB == nil {
		return fmt.Errorf("%s: %s", code, message)
	}

	if rc.ReleaseSlot != nil {
		defer rc.ReleaseSlot()
	}

	failed := rc.Emitter.Emit("run.failed", map[string]any{
		"error_class": errorClass,
		"code":        code,
		"message":     message,
		"details":     details,
	}, nil, StringPtr(errorClass))

	tx, err := rc.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := eventsRepo.AppendRunEvent(ctx, tx, rc.Run.ID, failed); err != nil {
		return err
	}
	if err := rc.RunStatusDB.UpdateRunTerminalStatus(ctx, tx, rc.Run.ID, data.TerminalStatusUpdate{Status: "failed"}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	publishRunEvent(ctx, rc)
	return nil
}

func publishRunEvent(ctx context.Context, rc *RunContext) {
	if rc == nil {
		return
	}
	channel := fmt.Sprintf("run_events:%s", rc.Run.ID.String())
	if rc.EventBus != nil {
		_ = rc.EventBus.Publish(ctx, channel, "")
	} else if rc.Pool != nil {
		_, _ = rc.Pool.Exec(ctx, "SELECT pg_notify($1, '')", channel)
	}
	if rc.BroadcastRDB != nil {
		redisChannel := fmt.Sprintf("arkloop:sse:run_events:%s", rc.Run.ID.String())
		_, _ = rc.BroadcastRDB.Publish(ctx, redisChannel, "").Result()
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

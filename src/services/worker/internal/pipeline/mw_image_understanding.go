package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/routing"
)

type ImageUnderstandingConfig struct {
	AuxGateway          llm.Gateway
	EmitDebugEvents     bool
	RoutingConfigLoader *routing.ConfigLoader
	EventsRepo          CompactRunEventAppender
}

// NewImageUnderstandingMiddleware 检测消息中是否包含图片，
// 若当前路由不支持视觉则用 vision 模型做 pre-description，将描述文字注入 context。
// 主模型路由不变（pre-description 模式，非 route swap）。
func NewImageUnderstandingMiddleware(cfg ImageUnderstandingConfig) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if rc == nil || !messageContainsImage(rc.Messages) {
			return next(ctx, rc)
		}

		if routeSupportsVision(rc.SelectedRoute) {
			return next(ctx, rc)
		}

		resolution, ok := resolveVisionRoute(ctx, rc.DB, rc.Run.AccountID,
			rc.AgentConfig.ImageModel,
			cfg.AuxGateway, cfg.EmitDebugEvents, rc.LlmMaxResponseBytes,
			cfg.RoutingConfigLoader, rc.RoutingByokEnabled)
		if !ok {
			// graceful degradation: 无 vision route 时跳过描述，
			// 图片仍传给主模型（可能被忽略），不 hard fail。
			slog.WarnContext(ctx, "image_understanding: no vision route, skipping image description",
				"current_model", modelFromSelectedRoute(rc.SelectedRoute))
			return next(ctx, rc)
		}

		descriptions, err := describeImagesWithGateway(ctx, rc, resolution.Gateway, resolution.Selected.Route.Model)
		if err != nil {
			slog.WarnContext(ctx, "image_understanding: description failed, continuing without descriptions",
				"vision_model", resolution.Selected.Route.Model, "err", err.Error())
			return next(ctx, rc)
		}

		if len(descriptions) > 0 {
			injectImageDescriptions(rc, descriptions)
			slog.InfoContext(ctx, "image_understanding: injected image descriptions",
				"count", len(descriptions),
				"vision_model", resolution.Selected.Route.Model,
				"primary_model", modelFromSelectedRoute(rc.SelectedRoute))
		}

		return next(ctx, rc)
	}
}

// describeImagesWithGateway 调用 vision gateway 对消息中的图片做描述。
func describeImagesWithGateway(
	ctx context.Context,
	rc *RunContext,
	visionGateway llm.Gateway,
	visionModel string,
) ([]string, error) {
	var descriptions []string
	for i := range rc.Messages {
		hasImage := false
		for _, part := range rc.Messages[i].Content {
			if part.Kind() == "image" {
				hasImage = true
				break
			}
		}
		if !hasImage {
			continue
		}
		desc, err := describeSingleImage(ctx, rc, visionGateway, visionModel, i)
		if err != nil {
			return descriptions, fmt.Errorf("describe image in message %d: %w", i, err)
		}
		descriptions = append(descriptions, desc)
	}
	return descriptions, nil
}

// describeSingleImage 用 vision 模型描述单张图片。
func describeSingleImage(
	ctx context.Context,
	rc *RunContext,
	visionGateway llm.Gateway,
	visionModel string,
	msgIndex int,
) (string, error) {
	// 构造只包含当前用户消息的图片部分的请求
	var imageParts []llm.ContentPart
	for _, part := range rc.Messages[msgIndex].Content {
		if part.Kind() == "image" {
			imageParts = append(imageParts, part)
		}
	}
	if len(imageParts) == 0 {
		return "", nil
	}

	maxTokens := 1024
	req := llm.Request{
		Model: visionModel,
		Messages: []llm.Message{
			{Role: "user", Content: append([]llm.ContentPart{
				llm.ContentPart{Type: "text", Text: "Describe this image in detail. Focus on the visual content, text visible in the image, and any relevant context. Be concise but thorough."},
			}, imageParts...)},
		},
		MaxOutputTokens: &maxTokens,
	}

	var result strings.Builder
	err := visionGateway.Stream(ctx, req, func(event llm.StreamEvent) error {
		switch ev := event.(type) {
		case llm.StreamMessageDelta:
			if ev.Channel == nil || *ev.Channel == "default" {
				result.WriteString(ev.ContentDelta)
			}
		case llm.StreamRunFailed:
			return fmt.Errorf("vision model failed: %s", ev.Error.Message)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result.String()), nil
}

// injectImageDescriptions 将图片描述作为 system-like 注入消息列表开头。
func injectImageDescriptions(rc *RunContext, descriptions []string) {
	var textParts []llm.ContentPart
	for _, desc := range descriptions {
		textParts = append(textParts, llm.ContentPart{
			Type: "text",
			Text: desc,
		})
	}
	if len(textParts) == 0 {
		return
	}

	block := llm.ContentPart{
		Type: "text",
		Text: "<image_descriptions>\n" + strings.Join(descriptions, "\n") + "\n</image_descriptions>",
	}

	// 插入到第一条 user 消息之前
	inserted := false
	for i, msg := range rc.Messages {
		if msg.Role == "user" {
			rc.Messages[i].Content = append([]llm.ContentPart{block}, rc.Messages[i].Content...)
			inserted = true
			break
		}
	}
	if !inserted {
		rc.Messages = append([]llm.Message{{Role: "user", Content: []llm.ContentPart{block}}}, rc.Messages...)
	}
}

func modelFromSelectedRoute(selected *routing.SelectedProviderRoute) string {
	if selected == nil {
		return ""
	}
	return selected.Route.Model
}

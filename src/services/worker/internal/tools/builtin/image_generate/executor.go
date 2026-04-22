package imagegenerate

import (
	"context"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	sharedconfig "arkloop/services/shared/config"
	"arkloop/services/shared/messagecontent"
	"arkloop/services/shared/objectstore"
	workerdata "arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/routing"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
)

const (
	imageGenerateConfigKey    = "image_generative.model"
	defaultGeneratedImageName = "generated-image"
)

type ToolExecutor struct {
	store         objectstore.Store
	db            workerdata.QueryDB
	config        sharedconfig.Resolver
	routingLoader *routing.ConfigLoader
	generate      func(context.Context, llm.ResolvedGatewayConfig, llm.ImageGenerationRequest) (llm.GeneratedImage, error)
}

func NewToolExecutor(
	store objectstore.Store,
	db workerdata.QueryDB,
	config sharedconfig.Resolver,
	routingLoader *routing.ConfigLoader,
) *ToolExecutor {
	return &ToolExecutor{
		store:         store,
		db:            db,
		config:        config,
		routingLoader: routingLoader,
		generate:      llm.GenerateImageWithResolvedConfig,
	}
}

func (e *ToolExecutor) Execute(
	ctx context.Context,
	_ string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	_ string,
) tools.ExecutionResult {
	started := time.Now()
	if e == nil || e.store == nil {
		return errResult("tool.not_configured", "image generation storage is not configured", started)
	}
	if execCtx.AccountID == nil {
		return errResult("tool.execution_failed", "account context is required", started)
	}

	prompt := strings.TrimSpace(stringArg(args, "prompt"))
	if prompt == "" {
		return errResult("tool.args_invalid", "parameter prompt is required", started)
	}
	inputImages, err := e.loadInputImages(ctx, args, *execCtx.AccountID)
	if err != nil {
		return errResult("tool.args_invalid", err.Error(), started)
	}
	selected, err := e.resolveSelectedRoute(ctx, *execCtx.AccountID)
	if err != nil {
		return errResult("tool.not_configured", err.Error(), started)
	}
	request := llm.ImageGenerationRequest{
		Prompt:       prompt,
		InputImages:  inputImages,
		Size:         strings.TrimSpace(stringArg(args, "size")),
		Quality:      strings.TrimSpace(stringArg(args, "quality")),
		Background:   strings.TrimSpace(stringArg(args, "background")),
		OutputFormat: strings.TrimSpace(stringArg(args, "output_format")),
	}
	caps := routing.SelectedRouteModelCapabilities(selected)
	if selected.Credential.ProviderKind == routing.ProviderKindOpenAI && (caps.ModelType == "image" || (caps.SupportsOutputModality("image") && !caps.SupportsOutputModality("text"))) {
		request.ForceOpenAIImageAPI = true
	}
	resolved, err := pipeline.ResolveGatewayConfigFromSelectedRoute(*selected, false, 0)
	if err != nil {
		return errResult("tool.execution_failed", fmt.Sprintf("resolve image model failed: %s", err.Error()), started)
	}

	generator := e.generate
	if generator == nil {
		generator = llm.GenerateImageWithResolvedConfig
	}
	image, err := generator(ctx, resolved, request)
	if err != nil {
		return errResultWithDetails(errorClassForGenerateError(err), err.Error(), errorDetailsForGenerateError(err), started)
	}
	if len(image.Bytes) == 0 {
		return errResult("tool.execution_failed", "image provider returned empty image bytes", started)
	}

	contentType := normalizeImageContentType(image.MimeType, image.Bytes)
	filename := defaultGeneratedImageName + fileExtForContentType(contentType)
	key := buildArtifactKey(execCtx, filename)
	var threadID *string
	if execCtx.ThreadID != nil {
		value := execCtx.ThreadID.String()
		threadID = &value
	}
	metadata := objectstore.ArtifactMetadata(objectstore.ArtifactOwnerKindRun, execCtx.RunID.String(), execCtx.AccountID.String(), threadID)
	if err := e.store.PutObject(ctx, key, image.Bytes, objectstore.PutOptions{
		ContentType: contentType,
		Metadata:    metadata,
	}); err != nil {
		return errResult("tool.upload_failed", fmt.Sprintf("save generated image failed: %s", err.Error()), started)
	}

	result := map[string]any{
		"provider":  image.ProviderKind,
		"model":     image.Model,
		"mime_type": contentType,
		"bytes":     len(image.Bytes),
		"artifacts": []map[string]any{
			{
				"key":       key,
				"filename":  filename,
				"size":      len(image.Bytes),
				"mime_type": contentType,
				"title":     defaultGeneratedImageName,
				"display":   "inline",
			},
		},
	}
	if strings.TrimSpace(image.RevisedPrompt) != "" {
		result["revised_prompt"] = strings.TrimSpace(image.RevisedPrompt)
	}

	return tools.ExecutionResult{
		ResultJSON: result,
		DurationMs: durationMs(started),
	}
}

func (e *ToolExecutor) IsAvailableForAccount(ctx context.Context, accountID uuid.UUID) bool {
	if accountID == uuid.Nil {
		return false
	}
	_, err := e.resolveSelectedRoute(ctx, accountID)
	return err == nil
}

func (e *ToolExecutor) resolveSelectedRoute(ctx context.Context, accountID uuid.UUID) (*routing.SelectedProviderRoute, error) {
	if e.routingLoader == nil {
		return nil, fmt.Errorf("image generation routing is not configured")
	}
	selector := ""
	if e.db != nil {
		_ = e.db.QueryRow(ctx,
			`SELECT value FROM account_entitlement_overrides
			  WHERE account_id = $1 AND key = $2
			    AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)
			  LIMIT 1`,
			accountID, imageGenerateConfigKey,
		).Scan(&selector)
	}
	selector = strings.TrimSpace(selector)
	if selector == "" && e.config != nil {
		if value, err := e.config.Resolve(ctx, imageGenerateConfigKey, sharedconfig.Scope{}); err == nil {
			selector = strings.TrimSpace(value)
		}
	}
	if selector == "" {
		return nil, fmt.Errorf("image generation model is not configured")
	}

	cfg, err := e.routingLoader.Load(ctx, &accountID)
	if err != nil {
		return nil, fmt.Errorf("load image routing config failed: %w", err)
	}
	if len(cfg.Routes) == 0 {
		return nil, fmt.Errorf("image routing config is empty")
	}

	credName, modelName, exact := splitModelSelector(selector)
	if exact {
		if route, cred, ok := cfg.GetHighestPriorityRouteByCredentialAndModel(credName, modelName, map[string]any{}); ok {
			return &routing.SelectedProviderRoute{Route: route, Credential: cred}, nil
		}
		if route, cred, ok := cfg.GetHighestPriorityRouteByCredentialName(credName, map[string]any{}); ok {
			route.Model = modelName
			return &routing.SelectedProviderRoute{Route: route, Credential: cred}, nil
		}
		return nil, fmt.Errorf("image generation route not found for selector: %s", selector)
	}
	if route, cred, ok := cfg.GetHighestPriorityRouteByModel(selector, map[string]any{}); ok {
		return &routing.SelectedProviderRoute{Route: route, Credential: cred}, nil
	}
	return nil, fmt.Errorf("image generation route not found for selector: %s", selector)
}

func splitModelSelector(selector string) (string, string, bool) {
	parts := strings.SplitN(strings.TrimSpace(selector), "^", 2)
	if len(parts) != 2 {
		return "", strings.TrimSpace(selector), false
	}
	credentialName := strings.TrimSpace(parts[0])
	modelName := strings.TrimSpace(parts[1])
	if credentialName == "" || modelName == "" {
		return "", strings.TrimSpace(selector), false
	}
	return credentialName, modelName, true
}

func buildArtifactKey(execCtx tools.ExecutionContext, filename string) string {
	accountID := "_anonymous"
	if execCtx.AccountID != nil {
		accountID = execCtx.AccountID.String()
	}
	return filepath.ToSlash(fmt.Sprintf("%s/%s/%s", accountID, execCtx.RunID.String(), filename))
}

func (e *ToolExecutor) loadInputImages(ctx context.Context, args map[string]any, accountID uuid.UUID) ([]llm.ContentPart, error) {
	if e == nil || e.store == nil || args == nil {
		return nil, nil
	}
	rawValues, ok := args["input_images"]
	if !ok || rawValues == nil {
		return nil, nil
	}
	items, ok := rawValues.([]any)
	if !ok {
		return nil, fmt.Errorf("parameter input_images must be an array of artifact references")
	}
	parts := make([]llm.ContentPart, 0, len(items))
	for idx, item := range items {
		raw, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("input_images[%d] must be a string", idx)
		}
		key := normalizeArtifactRef(raw)
		if key == "" {
			return nil, fmt.Errorf("input_images[%d] is empty", idx)
		}
		if !artifactKeyMatchesAccount(key, accountID) {
			return nil, fmt.Errorf("input_images[%d] is outside the current account", idx)
		}
		data, contentType, err := e.store.GetWithContentType(ctx, key)
		if err != nil {
			return nil, fmt.Errorf("input_images[%d] not found", idx)
		}
		detectedType := httpDetectContentType(data)
		rawType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
		if !strings.HasPrefix(rawType, "image/") && !strings.HasPrefix(detectedType, "image/") {
			return nil, fmt.Errorf("input_images[%d] is not an image artifact", idx)
		}
		contentType = normalizeImageContentType(contentType, data)
		parts = append(parts, llm.ContentPart{
			Type: "image",
			Attachment: &messagecontent.AttachmentRef{
				Key:      key,
				Filename: filepath.Base(key),
				MimeType: contentType,
				Size:     int64(len(data)),
			},
			Data: data,
		})
	}
	return parts, nil
}

func normalizeArtifactRef(value string) string {
	trimmed := strings.TrimSpace(value)
	if strings.HasPrefix(trimmed, "artifact:") {
		return strings.TrimSpace(strings.TrimPrefix(trimmed, "artifact:"))
	}
	return trimmed
}

func artifactKeyMatchesAccount(key string, accountID uuid.UUID) bool {
	key = strings.TrimSpace(key)
	if key == "" || accountID == uuid.Nil {
		return false
	}
	return strings.HasPrefix(key, accountID.String()+"/")
}

func normalizeImageContentType(contentType string, data []byte) string {
	cleaned := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if strings.HasPrefix(cleaned, "image/") {
		return cleaned
	}
	if detected := httpDetectContentType(data); strings.HasPrefix(detected, "image/") {
		return detected
	}
	return "image/png"
}

func httpDetectContentType(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(strings.Split(http.DetectContentType(data), ";")[0]))
}

func fileExtForContentType(contentType string) string {
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		exts, err := mime.ExtensionsByType(contentType)
		if err == nil && len(exts) > 0 {
			return exts[0]
		}
		return ".png"
	}
}

func errorClassForGenerateError(err error) string {
	if err == nil {
		return "tool.execution_failed"
	}
	if gatewayErr, ok := err.(llm.GatewayError); ok {
		return gatewayErr.ErrorClass
	}
	if gatewayErr, ok := err.(*llm.GatewayError); ok && gatewayErr != nil {
		return gatewayErr.ErrorClass
	}
	return "tool.execution_failed"
}

func errorDetailsForGenerateError(err error) map[string]any {
	if err == nil {
		return nil
	}
	if gatewayErr, ok := err.(llm.GatewayError); ok {
		return copyMap(gatewayErr.Details)
	}
	if gatewayErr, ok := err.(*llm.GatewayError); ok && gatewayErr != nil {
		return copyMap(gatewayErr.Details)
	}
	return nil
}

func stringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	if value, ok := args[key].(string); ok {
		return value
	}
	return ""
}

func errResult(errorClass, message string, started time.Time) tools.ExecutionResult {
	return errResultWithDetails(errorClass, message, nil, started)
}

func errResultWithDetails(errorClass, message string, details map[string]any, started time.Time) tools.ExecutionResult {
	return tools.ExecutionResult{
		Error: &tools.ExecutionError{
			ErrorClass: errorClass,
			Message:    message,
			Details:    copyMap(details),
		},
		DurationMs: durationMs(started),
	}
}

func copyMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func durationMs(started time.Time) int {
	elapsed := time.Since(started)
	if elapsed < 0 {
		return 0
	}
	return int(elapsed / time.Millisecond)
}

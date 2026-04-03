package read

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	sharedoutbound "arkloop/services/shared/outboundurl"
	"arkloop/services/shared/toolruntime"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin/fileops"
)

const (
	errorArgsInvalid      = "tool.args_invalid"
	errorFetchFailed      = "tool.fetch_failed"
	errorNotConfigured    = "config.missing"
	errorProviderFailed   = "external_provider.failed"
	errorTimeout          = "tool.timeout"
	errorTooLarge         = "tool.payload_too_large"
	errorUnsupportedMedia = "tool.unsupported_media_type"
	errorURLDenied        = "tool.url_denied"
	defaultTimeout        = 30 * time.Second
	defaultMaxBytes       = 20 * 1024 * 1024
	maxTimeoutMs          = 120000
)

type sourceKind string

const (
	sourceKindFilePath          sourceKind = "file_path"
	sourceKindMessageAttachment sourceKind = "message_attachment"
	sourceKindRemoteURL         sourceKind = "remote_url"
)

type sourceArgs struct {
	Kind          sourceKind
	FilePath      string
	AttachmentKey string
	URL           string
}

type parsedArgs struct {
	Source          sourceArgs
	Prompt          string
	Offset          int
	Limit           int
	MaxBytes        int
	TimeoutOverride *int
}

type AttachmentFetcher interface {
	GetWithContentType(ctx context.Context, key string) ([]byte, string, error)
}

type Executor struct {
	Tracker         *fileops.FileTracker
	AttachmentStore AttachmentFetcher

	provider Provider
	timeout  time.Duration

	providerMu    sync.Mutex
	providerCache map[string]Provider
}

type readToolMessageProvider interface {
	ReadToolMessages() []llm.Message
}

func NewToolExecutor() *Executor {
	return &Executor{
		timeout:       defaultTimeout,
		providerCache: map[string]Provider{},
	}
}

func NewToolExecutorWithProvider(provider Provider) *Executor {
	return &Executor{
		provider:      provider,
		timeout:       defaultTimeout,
		providerCache: map[string]Provider{},
	}
}

func NewToolExecutorWithTracker(tracker *fileops.FileTracker) *Executor {
	return &Executor{
		Tracker:       tracker,
		timeout:       defaultTimeout,
		providerCache: map[string]Provider{},
	}
}

func (e *Executor) IsNotConfigured() bool {
	return false
}

func (e *Executor) Execute(
	ctx context.Context,
	toolName string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	_ string,
) tools.ExecutionResult {
	_ = toolName
	started := time.Now()

	parsed, argErr := parseArgs(args)
	if argErr != nil {
		return tools.ExecutionResult{Error: argErr, DurationMs: durationMs(started)}
	}

	switch parsed.Source.Kind {
	case sourceKindFilePath:
		return e.executeFilePath(ctx, parsed, execCtx, started)
	case sourceKindMessageAttachment:
		return e.executeMessageAttachment(ctx, parsed, execCtx, started)
	case sourceKindRemoteURL:
		return e.executeRemoteURL(ctx, parsed, execCtx, started)
	default:
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: errorArgsInvalid,
				Message:    "unsupported source.kind",
				Details:    map[string]any{"source_kind": string(parsed.Source.Kind)},
			},
			DurationMs: durationMs(started),
		}
	}
}

func (e *Executor) executeFilePath(
	ctx context.Context,
	parsed parsedArgs,
	execCtx tools.ExecutionContext,
	started time.Time,
) tools.ExecutionResult {
	if parsed.Prompt != "" || parsed.TimeoutOverride != nil || parsed.MaxBytes != defaultMaxBytes {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: errorArgsInvalid,
				Message:    "file_path source does not accept prompt/max_bytes/timeout_ms",
			},
			DurationMs: durationMs(started),
		}
	}

	filePath := parsed.Source.FilePath
	backend := fileops.ResolveBackend(execCtx.RuntimeSnapshot, execCtx.WorkDir, execCtx.RunID.String(), resolveAccountID(execCtx), execCtx.ProfileRef, execCtx.WorkspaceRef)

	info, err := backend.Stat(ctx, filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fileErrorResult(fmt.Sprintf("file not found: %s", filePath), started)
		}
		return fileErrorResult(fmt.Sprintf("stat failed: %s", err.Error()), started)
	}
	if info.IsDir {
		return fileErrorResult(fmt.Sprintf("path is a directory: %s", filePath), started)
	}
	if info.Size > int64(fileops.MaxReadSize) {
		return fileErrorResult(fmt.Sprintf("file too large (%d bytes, max %d)", info.Size, fileops.MaxReadSize), started)
	}

	data, err := backend.ReadFile(ctx, filePath)
	if err != nil {
		return fileErrorResult(fmt.Sprintf("read failed: %s", err.Error()), started)
	}

	content, totalLines, truncated := fileops.ReadLines(data, parsed.Offset-1, parsed.Limit)
	numbered := fileops.FormatWithLineNumbers(content, parsed.Offset)

	if e.Tracker != nil {
		e.Tracker.RecordRead(filePath)
	}

	result := numbered
	if truncated {
		result += fmt.Sprintf("\n\n(showing lines %d-%d of %d; use offset to read further)", parsed.Offset, parsed.Offset+parsed.Limit-1, totalLines)
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"content":     result,
			"file_path":   filePath,
			"total_lines": totalLines,
			"truncated":   truncated,
		},
		DurationMs: durationMs(started),
	}
}

func (e *Executor) executeMessageAttachment(
	ctx context.Context,
	parsed parsedArgs,
	execCtx tools.ExecutionContext,
	started time.Time,
) tools.ExecutionResult {
	if parsed.Source.URL != "" {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: errorArgsInvalid,
				Message:    "message_attachment source does not accept url",
			},
			DurationMs: durationMs(started),
		}
	}

	timeout := resolveTimeout(e.timeout, execCtx.TimeoutMs, parsed.TimeoutOverride)
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	image, err := fetchMessageAttachmentImage(execCtx.PipelineRC, parsed.Source.AttachmentKey, parsed.MaxBytes, e.AttachmentStore)
	if err != nil {
		return tools.ExecutionResult{
			Error:      executionErrorFromFetchError(err),
			DurationMs: durationMs(started),
		}
	}

	provider, providerErr := e.resolveProvider(execCtx)
	if providerErr != nil {
		return tools.ExecutionResult{
			ResultJSON: map[string]any{
				"source_kind":    string(parsed.Source.Kind),
				"attachment_key": parsed.Source.AttachmentKey,
				"mime_type":      image.MimeType,
				"bytes":          len(image.Bytes),
			},
			ContentParts: []tools.ContentAttachment{
				{MimeType: image.MimeType, Data: image.Bytes},
			},
			DurationMs: durationMs(started),
		}
	}

	description, err := provider.DescribeImage(runCtx, DescribeImageRequest{
		Prompt:    parsed.Prompt,
		SourceURL: image.SourceURL,
		MimeType:  image.MimeType,
		Bytes:     image.Bytes,
	})
	if err != nil {
		return tools.ExecutionResult{
			Error:      executionErrorFromProviderError(err),
			DurationMs: durationMs(started),
		}
	}

	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"text":           description.Text,
			"provider":       description.Provider,
			"model":          description.Model,
			"source_kind":    string(parsed.Source.Kind),
			"attachment_key": parsed.Source.AttachmentKey,
			"mime_type":      image.MimeType,
			"bytes":          len(image.Bytes),
		},
		DurationMs: durationMs(started),
	}
}

func (e *Executor) executeRemoteURL(
	ctx context.Context,
	parsed parsedArgs,
	execCtx tools.ExecutionContext,
	started time.Time,
) tools.ExecutionResult {
	if parsed.Source.AttachmentKey != "" {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: errorArgsInvalid,
				Message:    "remote_url source does not accept attachment_key",
			},
			DurationMs: durationMs(started),
		}
	}

	provider, providerErr := e.resolveProvider(execCtx)
	if providerErr != nil {
		return tools.ExecutionResult{Error: providerErr, DurationMs: durationMs(started)}
	}

	if err := sharedoutbound.DefaultPolicy().ValidateRequestURL(parsed.Source.URL); err != nil {
		return tools.ExecutionResult{
			Error:      executionErrorFromFetchError(err),
			DurationMs: durationMs(started),
		}
	}

	timeout := resolveTimeout(e.timeout, execCtx.TimeoutMs, parsed.TimeoutOverride)
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	image, err := fetchRemoteImage(runCtx, parsed.Source.URL, parsed.MaxBytes)
	if err != nil {
		return tools.ExecutionResult{
			Error:      executionErrorFromFetchError(err),
			DurationMs: durationMs(started),
		}
	}

	description, err := provider.DescribeImage(runCtx, DescribeImageRequest{
		Prompt:    parsed.Prompt,
		SourceURL: image.FinalURL,
		MimeType:  image.MimeType,
		Bytes:     image.Bytes,
	})
	if err != nil {
		return tools.ExecutionResult{
			Error:      executionErrorFromProviderError(err),
			DurationMs: durationMs(started),
		}
	}

	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"text":        description.Text,
			"provider":    description.Provider,
			"model":       description.Model,
			"source_kind": string(parsed.Source.Kind),
			"source_url":  image.FinalURL,
			"mime_type":   image.MimeType,
			"bytes":       len(image.Bytes),
		},
		DurationMs: durationMs(started),
	}
}

func (e *Executor) resolveProvider(execCtx tools.ExecutionContext) (Provider, *tools.ExecutionError) {
	if e != nil && e.provider != nil {
		return e.provider, nil
	}

	if e == nil {
		return nil, &tools.ExecutionError{
			ErrorClass: errorNotConfigured,
			Message:    "read image backend not configured",
			Details:    map[string]any{"group_name": GroupName},
		}
	}

	if cfg, ok := execCtx.ActiveToolProviderConfigsByGroup[GroupName]; ok {
		provider, err := e.providerFromConfig(cfg)
		if err != nil {
			return nil, &tools.ExecutionError{
				ErrorClass: errorNotConfigured,
				Message:    "read image backend not configured",
				Details: map[string]any{
					"group_name":    GroupName,
					"provider_name": strings.TrimSpace(cfg.ProviderName),
					"reason":        err.Error(),
				},
			}
		}
		return provider, nil
	}

	if execCtx.RuntimeSnapshot != nil {
		for _, cfg := range execCtx.RuntimeSnapshot.PlatformProviders {
			if strings.TrimSpace(cfg.GroupName) != GroupName {
				continue
			}
			provider, err := e.providerFromConfig(cfg)
			if err != nil {
				continue
			}
			return provider, nil
		}
	}

	return nil, &tools.ExecutionError{
		ErrorClass: errorNotConfigured,
		Message:    "read image backend not configured",
		Details:    map[string]any{"group_name": GroupName},
	}
}

func (e *Executor) providerFromConfig(cfg toolruntime.ProviderConfig) (Provider, error) {
	name := strings.TrimSpace(cfg.ProviderName)
	if name == "" {
		return nil, fmt.Errorf("provider_name is required")
	}

	e.providerMu.Lock()
	defer e.providerMu.Unlock()
	if provider, ok := e.providerCache[name]; ok {
		return provider, nil
	}

	switch name {
	case ProviderNameMiniMax:
		key := ""
		if cfg.APIKeyValue != nil {
			key = strings.TrimSpace(*cfg.APIKeyValue)
		}
		if key == "" {
			return nil, fmt.Errorf("api_key is required")
		}
		baseURL := ""
		if cfg.BaseURL != nil {
			baseURL = strings.TrimSpace(*cfg.BaseURL)
		}
		model := DefaultMiniMaxModel
		if rawModel, ok := cfg.ConfigJSON["model"].(string); ok && strings.TrimSpace(rawModel) != "" {
			model = strings.TrimSpace(rawModel)
		}
		provider, err := NewMiniMaxProvider(key, baseURL, model)
		if err != nil {
			return nil, err
		}
		e.providerCache[name] = provider
		return provider, nil
	default:
		return nil, fmt.Errorf("unsupported provider: %s", name)
	}
}

func fetchMessageAttachmentImage(pipelineRC any, attachmentKey string, maxBytes int, store AttachmentFetcher) (fetchedImage, error) {
	key := strings.TrimSpace(attachmentKey)
	if key == "" {
		return fetchedImage{}, fmt.Errorf("attachment key is required")
	}
	messages := messagesFromPipelineRC(pipelineRC)
	if len(messages) == 0 {
		return fetchedImage{}, fmt.Errorf("message attachment not found")
	}
	for _, msg := range messages {
		for _, part := range msg.Content {
			if part.Kind() != "image" || part.Attachment == nil {
				continue
			}
			if strings.TrimSpace(part.Attachment.Key) != key {
				continue
			}
			if len(part.Data) == 0 {
				return fetchedImage{}, fmt.Errorf("message attachment image data is empty")
			}
			if len(part.Data) > maxBytes {
				return fetchedImage{}, imageTooLargeError{MaxBytes: maxBytes}
			}
			mimeType := detectImageMimeType(part.Attachment.MimeType, part.Data)
			if mimeType == "" {
				return fetchedImage{}, unsupportedMediaTypeError{
					DetectedMimeType: detectedMimeType(part.Attachment.MimeType, part.Data),
				}
			}
			return fetchedImage{
				SourceURL: "attachment:" + key,
				FinalURL:  "attachment:" + key,
				MimeType:  mimeType,
				Bytes:     part.Data,
			}, nil
		}
	}

	// fallback: load from object store by key
	if store != nil {
		data, contentType, err := store.GetWithContentType(context.Background(), key)
		if err == nil && len(data) > 0 {
			if len(data) > maxBytes {
				return fetchedImage{}, imageTooLargeError{MaxBytes: maxBytes}
			}
			mimeType := detectImageMimeType(contentType, data)
			if mimeType == "" {
				return fetchedImage{}, unsupportedMediaTypeError{DetectedMimeType: detectedMimeType(contentType, data)}
			}
			return fetchedImage{
				SourceURL: "attachment:" + key,
				FinalURL:  "attachment:" + key,
				MimeType:  mimeType,
				Bytes:     data,
			}, nil
		}
	}

	return fetchedImage{}, fmt.Errorf("message attachment not found")
}

func messagesFromPipelineRC(pipelineRC any) []llm.Message {
	provider, ok := pipelineRC.(readToolMessageProvider)
	if !ok || provider == nil {
		return nil
	}
	messages := provider.ReadToolMessages()
	if len(messages) == 0 {
		return nil
	}
	return messages
}

func parseArgs(args map[string]any) (parsedArgs, *tools.ExecutionError) {
	unknown := make([]string, 0)
	for key := range args {
		if key != "source" && key != "prompt" && key != "offset" && key != "limit" && key != "max_bytes" && key != "timeout_ms" {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return parsedArgs{}, &tools.ExecutionError{
			ErrorClass: errorArgsInvalid,
			Message:    "tool arguments do not allow extra fields",
			Details:    map[string]any{"unknown_fields": unknown},
		}
	}

	rawSource, ok := args["source"].(map[string]any)
	if !ok {
		return parsedArgs{}, requiredObjectArgError("source")
	}
	source, err := parseSource(rawSource)
	if err != nil {
		return parsedArgs{}, err
	}

	prompt := ""
	if rawPrompt, exists := args["prompt"]; exists {
		asString, ok := rawPrompt.(string)
		if !ok {
			return parsedArgs{}, requiredStringArgError("prompt")
		}
		prompt = strings.TrimSpace(asString)
	}

	offset := intArg(args, "offset", 1)
	limit := intArg(args, "limit", fileops.DefaultReadLimit)
	if offset < 1 {
		offset = 1
	}
	if limit < 1 {
		limit = fileops.DefaultReadLimit
	}

	maxBytes := defaultMaxBytes
	if raw, exists := args["max_bytes"]; exists {
		value, ok := intOnlyArg(raw)
		if !ok || value <= 0 || value > defaultMaxBytes {
			return parsedArgs{}, &tools.ExecutionError{
				ErrorClass: errorArgsInvalid,
				Message:    fmt.Sprintf("parameter max_bytes must be in range 1..%d", defaultMaxBytes),
				Details:    map[string]any{"field": "max_bytes", "max": defaultMaxBytes},
			}
		}
		maxBytes = value
	}

	var timeoutOverride *int
	if raw, exists := args["timeout_ms"]; exists {
		value, ok := intOnlyArg(raw)
		if !ok || value <= 0 || value > maxTimeoutMs {
			return parsedArgs{}, &tools.ExecutionError{
				ErrorClass: errorArgsInvalid,
				Message:    fmt.Sprintf("parameter timeout_ms must be in range 1..%d", maxTimeoutMs),
				Details:    map[string]any{"field": "timeout_ms", "max": maxTimeoutMs},
			}
		}
		timeoutOverride = &value
	}

	switch source.Kind {
	case sourceKindFilePath:
		if source.FilePath == "" {
			return parsedArgs{}, requiredStringArgError("source.file_path")
		}
	case sourceKindMessageAttachment:
		if source.AttachmentKey == "" {
			return parsedArgs{}, requiredStringArgError("source.attachment_key")
		}
	case sourceKindRemoteURL:
		if source.URL == "" {
			return parsedArgs{}, requiredStringArgError("source.url")
		}
		if prompt == "" {
			return parsedArgs{}, requiredStringArgError("prompt")
		}
	default:
		return parsedArgs{}, &tools.ExecutionError{
			ErrorClass: errorArgsInvalid,
			Message:    "source.kind must be one of: file_path, message_attachment, remote_url",
			Details:    map[string]any{"field": "source.kind"},
		}
	}

	return parsedArgs{
		Source:          source,
		Prompt:          prompt,
		Offset:          offset,
		Limit:           limit,
		MaxBytes:        maxBytes,
		TimeoutOverride: timeoutOverride,
	}, nil
}

func parseSource(raw map[string]any) (sourceArgs, *tools.ExecutionError) {
	unknown := make([]string, 0)
	for key := range raw {
		if key != "kind" && key != "file_path" && key != "attachment_key" && key != "url" {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return sourceArgs{}, &tools.ExecutionError{
			ErrorClass: errorArgsInvalid,
			Message:    "source does not allow extra fields",
			Details:    map[string]any{"unknown_fields": unknown},
		}
	}

	rawKind, ok := raw["kind"].(string)
	if !ok || strings.TrimSpace(rawKind) == "" {
		return sourceArgs{}, requiredStringArgError("source.kind")
	}
	source := sourceArgs{Kind: sourceKind(strings.TrimSpace(rawKind))}
	if rawValue, exists := raw["file_path"]; exists {
		value, ok := rawValue.(string)
		if !ok {
			return sourceArgs{}, requiredStringArgError("source.file_path")
		}
		source.FilePath = strings.TrimSpace(value)
	}
	if rawValue, exists := raw["attachment_key"]; exists {
		value, ok := rawValue.(string)
		if !ok {
			return sourceArgs{}, requiredStringArgError("source.attachment_key")
		}
		source.AttachmentKey = strings.TrimSpace(value)
	}
	if rawValue, exists := raw["url"]; exists {
		value, ok := rawValue.(string)
		if !ok {
			return sourceArgs{}, requiredStringArgError("source.url")
		}
		source.URL = strings.TrimSpace(value)
	}
	return source, nil
}

func requiredObjectArgError(field string) *tools.ExecutionError {
	return &tools.ExecutionError{
		ErrorClass: errorArgsInvalid,
		Message:    fmt.Sprintf("parameter %s must be an object", field),
		Details:    map[string]any{"field": field},
	}
}

func requiredStringArgError(field string) *tools.ExecutionError {
	return &tools.ExecutionError{
		ErrorClass: errorArgsInvalid,
		Message:    fmt.Sprintf("parameter %s must be a non-empty string", field),
		Details:    map[string]any{"field": field},
	}
}

func intArg(args map[string]any, key string, defaultVal int) int {
	raw, exists := args[key]
	if !exists {
		return defaultVal
	}
	value, ok := intOnlyArg(raw)
	if !ok {
		return defaultVal
	}
	return value
}

func intOnlyArg(raw any) (int, bool) {
	switch value := raw.(type) {
	case int:
		return value, true
	case float64:
		if value != float64(int(value)) {
			return 0, false
		}
		return int(value), true
	default:
		return 0, false
	}
}

func resolveTimeout(base time.Duration, inherited *int, override *int) time.Duration {
	timeout := base
	if inherited != nil && *inherited > 0 {
		timeout = time.Duration(*inherited) * time.Millisecond
	}
	if override != nil && *override > 0 {
		overrideDuration := time.Duration(*override) * time.Millisecond
		if timeout <= 0 || overrideDuration < timeout {
			timeout = overrideDuration
		}
	}
	return timeout
}

func executionErrorFromFetchError(err error) *tools.ExecutionError {
	if err == nil {
		return nil
	}
	var denied sharedoutbound.DeniedError
	if errors.As(err, &denied) {
		details := map[string]any{"reason": denied.Reason}
		for key, value := range denied.Details {
			details[key] = value
		}
		return &tools.ExecutionError{
			ErrorClass: errorURLDenied,
			Message:    "read URL denied by security policy",
			Details:    details,
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &tools.ExecutionError{
			ErrorClass: errorTimeout,
			Message:    "read timed out",
		}
	}
	var tooLarge imageTooLargeError
	if errors.As(err, &tooLarge) {
		return &tools.ExecutionError{
			ErrorClass: errorTooLarge,
			Message:    "image exceeds max_bytes limit",
			Details:    map[string]any{"max_bytes": tooLarge.MaxBytes},
		}
	}
	var unsupported unsupportedMediaTypeError
	if errors.As(err, &unsupported) {
		return &tools.ExecutionError{
			ErrorClass: errorUnsupportedMedia,
			Message:    "source did not resolve to a supported image",
			Details:    map[string]any{"mime_type": unsupported.DetectedMimeType},
		}
	}
	var statusErr httpStatusError
	if errors.As(err, &statusErr) {
		return &tools.ExecutionError{
			ErrorClass: errorFetchFailed,
			Message:    "read request failed",
			Details:    map[string]any{"status_code": statusErr.StatusCode},
		}
	}
	return &tools.ExecutionError{
		ErrorClass: errorFetchFailed,
		Message:    "read fetch failed",
		Details:    map[string]any{"reason": err.Error()},
	}
}

func executionErrorFromProviderError(err error) *tools.ExecutionError {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &tools.ExecutionError{
			ErrorClass: errorTimeout,
			Message:    "read timed out",
		}
	}
	details := map[string]any{}
	var providerErr ProviderError
	if errors.As(err, &providerErr) {
		if providerErr.StatusCode > 0 {
			details["status_code"] = providerErr.StatusCode
		}
		if strings.TrimSpace(providerErr.TraceID) != "" {
			details["trace_id"] = providerErr.TraceID
		}
		if providerErr.Provider != "" {
			details["provider"] = providerErr.Provider
		}
	}
	details["reason"] = err.Error()
	return &tools.ExecutionError{
		ErrorClass: errorProviderFailed,
		Message:    "image reader provider failed",
		Details:    details,
	}
}

func fileErrorResult(message string, started time.Time) tools.ExecutionResult {
	return tools.ExecutionResult{
		Error: &tools.ExecutionError{
			ErrorClass: "tool.file_error",
			Message:    message,
		},
		DurationMs: durationMs(started),
	}
}

func resolveAccountID(execCtx tools.ExecutionContext) string {
	if execCtx.AccountID == nil {
		return ""
	}
	return execCtx.AccountID.String()
}

func durationMs(started time.Time) int {
	elapsed := time.Since(started)
	if elapsed < 0 {
		return 0
	}
	return int(elapsed / time.Millisecond)
}

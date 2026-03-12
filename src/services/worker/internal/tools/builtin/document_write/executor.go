package documentwrite

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"arkloop/services/shared/objectstore"
	"arkloop/services/worker/internal/tools"
)

const (
	errorArgsInvalid  = "tool.args_invalid"
	errorUploadFailed = "tool.upload_failed"
)

// ToolExecutor 将 Markdown 内容直接上传到 S3，不经过 Sandbox。
type ToolExecutor struct {
	store interface {
		PutObject(ctx context.Context, key string, data []byte, options objectstore.PutOptions) error
	}
}

func NewToolExecutor(store interface {
	PutObject(ctx context.Context, key string, data []byte, options objectstore.PutOptions) error
}) *ToolExecutor {
	return &ToolExecutor{store: store}
}

func (e *ToolExecutor) Execute(
	ctx context.Context,
	_ string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	_ string,
) tools.ExecutionResult {
	started := time.Now()

	filename, _ := args["filename"].(string)
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return errResult(errorArgsInvalid, "parameter filename is required", started)
	}

	content, _ := args["content"].(string)
	if content == "" {
		return errResult(errorArgsInvalid, "parameter content is required", started)
	}

	var orgPrefix string
	if execCtx.AccountID != nil {
		orgPrefix = execCtx.AccountID.String()
	} else {
		orgPrefix = "_anonymous"
	}
	key := fmt.Sprintf("%s/%s/%s", orgPrefix, execCtx.RunID.String(), filename)
	var threadID *string
	if execCtx.ThreadID != nil {
		value := execCtx.ThreadID.String()
		threadID = &value
	}
	metadata := objectstore.ArtifactMetadata(objectstore.ArtifactOwnerKindRun, execCtx.RunID.String(), orgPrefix, threadID)

	contentType := "text/markdown"
	if ext := strings.ToLower(filepath.Ext(filename)); ext != ".md" {
		contentType = "text/plain"
	}

	if err := e.store.PutObject(ctx, key, []byte(content), objectstore.PutOptions{ContentType: contentType, Metadata: metadata}); err != nil {
		return errResult(errorUploadFailed, fmt.Sprintf("upload failed: %s", err.Error()), started)
	}

	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"artifacts": []map[string]any{
				{
					"key":       key,
					"filename":  filename,
					"size":      len(content),
					"mime_type": contentType,
				},
			},
		},
		DurationMs: durationMs(started),
	}
}

func errResult(errorClass, message string, started time.Time) tools.ExecutionResult {
	return tools.ExecutionResult{
		Error: &tools.ExecutionError{
			ErrorClass: errorClass,
			Message:    message,
		},
		DurationMs: durationMs(started),
	}
}

func durationMs(started time.Time) int {
	elapsed := time.Since(started)
	millis := int(elapsed / time.Millisecond)
	if millis < 0 {
		return 0
	}
	return millis
}

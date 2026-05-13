package resourcecopy

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"arkloop/services/shared/objectstore"
	"arkloop/services/worker/internal/tools"
	"arkloop/services/worker/internal/tools/builtin/fileops"
)

const (
	errorArgsInvalid    = "tool.args_invalid"
	errorFetchFailed    = "tool.fetch_failed"
	errorForbidden      = "tool.forbidden"
	errorStoreMissing   = "config.missing"
	errorWriteFailed    = "tool.write_failed"
	artifactURIPrefix   = "artifact:"
	attachmentURIPrefix = "attachment:"
)

type Executor struct {
	ArtifactStore   objectstore.Store
	AttachmentStore objectstore.Store
}

func NewExecutor(artifactStore objectstore.Store, attachmentStore objectstore.Store) *Executor {
	return &Executor{ArtifactStore: artifactStore, AttachmentStore: attachmentStore}
}

func (e *Executor) Execute(
	ctx context.Context,
	_ string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	_ string,
) tools.ExecutionResult {
	started := time.Now()
	sourceURI := strings.TrimSpace(stringArg(args, "source_uri"))
	targetPath := strings.TrimSpace(stringArg(args, "target_path"))
	if sourceURI == "" {
		return errorResult(errorArgsInvalid, "parameter source_uri is required", started)
	}
	if targetPath == "" {
		return errorResult(errorArgsInvalid, "parameter target_path is required", started)
	}
	if !filepath.IsAbs(targetPath) {
		return errorResult(errorArgsInvalid, "parameter target_path must be an absolute file path", started)
	}

	sourceKind, key, parseErr := parseSourceURI(sourceURI)
	if parseErr != nil {
		return errorResult(errorArgsInvalid, parseErr.Error(), started)
	}

	data, contentType, fetchErr := e.fetch(ctx, sourceKind, key, execCtx)
	if fetchErr != nil {
		return tools.ExecutionResult{Error: fetchErr, DurationMs: durationMs(started)}
	}

	backend := fileops.ResolveBackend(execCtx.RuntimeSnapshot, execCtx.WorkDir, execCtx.RunID.String(), accountIDString(execCtx), execCtx.ProfileRef, execCtx.WorkspaceRef)
	if err := backend.WriteFile(ctx, targetPath, data); err != nil {
		return errorResult(errorWriteFailed, fmt.Sprintf("write failed: %s", err.Error()), started)
	}

	filePath := backend.NormalizePath(targetPath)
	resultJSON := map[string]any{
		"source_uri":  sourceURI,
		"target_path": targetPath,
		"file_path":   filePath,
		"bytes":       len(data),
		"mime_type":   contentType,
	}
	return tools.ExecutionResult{
		ResultJSON: resultJSON,
		DurationMs: durationMs(started),
	}
}

func (e *Executor) fetch(ctx context.Context, sourceKind string, key string, execCtx tools.ExecutionContext) ([]byte, string, *tools.ExecutionError) {
	switch sourceKind {
	case "artifact":
		if e == nil || e.ArtifactStore == nil {
			return nil, "", &tools.ExecutionError{ErrorClass: errorStoreMissing, Message: "artifact storage is not configured"}
		}
		if err := authorizeObjectKey(ctx, e.ArtifactStore, key, execCtx, false); err != nil {
			return nil, "", err
		}
		return getObject(ctx, e.ArtifactStore, key)
	case "attachment":
		if e == nil || e.AttachmentStore == nil {
			return nil, "", &tools.ExecutionError{ErrorClass: errorStoreMissing, Message: "message attachment storage is not configured"}
		}
		if err := authorizeObjectKey(ctx, e.AttachmentStore, key, execCtx, true); err != nil {
			return nil, "", err
		}
		return getObject(ctx, e.AttachmentStore, key)
	default:
		return nil, "", &tools.ExecutionError{ErrorClass: errorArgsInvalid, Message: "unsupported resource URI scheme"}
	}
}

func getObject(ctx context.Context, store objectstore.Store, key string) ([]byte, string, *tools.ExecutionError) {
	data, contentType, err := store.GetWithContentType(ctx, key)
	if err != nil {
		return nil, "", &tools.ExecutionError{ErrorClass: errorFetchFailed, Message: fmt.Sprintf("read resource failed: %s", err.Error())}
	}
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/octet-stream"
	}
	return data, contentType, nil
}

func authorizeObjectKey(ctx context.Context, store objectstore.Store, key string, execCtx tools.ExecutionContext, attachment bool) *tools.ExecutionError {
	if strings.Contains(key, "..") || strings.TrimSpace(key) == "" {
		return &tools.ExecutionError{ErrorClass: errorArgsInvalid, Message: "invalid resource key"}
	}
	accountID := accountIDString(execCtx)
	if accountID == "" {
		return nil
	}
	info, err := store.Head(ctx, key)
	if err != nil {
		return &tools.ExecutionError{ErrorClass: errorFetchFailed, Message: fmt.Sprintf("read resource metadata failed: %s", err.Error())}
	}
	if metadataAccountID := strings.TrimSpace(info.Metadata[objectstore.ArtifactMetaAccountID]); metadataAccountID != "" {
		if metadataAccountID != accountID {
			return &tools.ExecutionError{ErrorClass: errorForbidden, Message: "resource belongs to a different account"}
		}
		return nil
	}
	if attachment {
		if strings.HasPrefix(key, "attachments/"+accountID+"/") || strings.HasPrefix(key, "staging/"+accountID+"/") {
			return nil
		}
	} else if strings.HasPrefix(key, accountID+"/") {
		return nil
	}
	return &tools.ExecutionError{ErrorClass: errorForbidden, Message: "resource account could not be verified"}
}

func parseSourceURI(value string) (string, string, error) {
	switch {
	case strings.HasPrefix(value, artifactURIPrefix):
		key := strings.TrimSpace(strings.TrimPrefix(value, artifactURIPrefix))
		if key == "" {
			return "", "", fmt.Errorf("artifact URI is missing a key")
		}
		return "artifact", key, nil
	case strings.HasPrefix(value, attachmentURIPrefix):
		key := strings.TrimSpace(strings.TrimPrefix(value, attachmentURIPrefix))
		if key == "" {
			return "", "", fmt.Errorf("attachment URI is missing a key")
		}
		return "attachment", key, nil
	default:
		return "", "", fmt.Errorf("source_uri must start with artifact: or attachment")
	}
}

func stringArg(args map[string]any, key string) string {
	value, _ := args[key].(string)
	return value
}

func accountIDString(execCtx tools.ExecutionContext) string {
	if execCtx.AccountID == nil {
		return ""
	}
	return execCtx.AccountID.String()
}

func errorResult(errorClass, message string, started time.Time) tools.ExecutionResult {
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

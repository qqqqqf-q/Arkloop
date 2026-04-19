package tools

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"

	"arkloop/services/worker/internal/tools/builtin/fileops"
)

const (
	PersistThreshold    = 16 * 1024 // 16KB
	PersistPreviewBytes = 2 * 1024  // 2KB
)

var validToolCallIDRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// PersistLargeResult writes tool outputs larger than PersistThreshold to disk
// and replaces ResultJSON with a lightweight preview containing the file path.
// The raw bytes must be the JSON-marshaled form of result.ResultJSON.
func PersistLargeResult(
	ctx context.Context,
	execCtx ExecutionContext,
	toolCallID string,
	raw []byte,
	result ExecutionResult,
) ExecutionResult {
	if result.ResultJSON == nil || len(raw) <= PersistThreshold {
		return result
	}
	toolCallID = strings.TrimSpace(toolCallID)
	if !validToolCallIDRe.MatchString(toolCallID) {
		slog.Warn("persist_large_result: invalid tool_call_id, skipping persistence", "tool_call_id", toolCallID)
		return result
	}

	accountID := ""
	if execCtx.AccountID != nil {
		accountID = execCtx.AccountID.String()
	}

	backend := fileops.ResolveBackend(
		execCtx.RuntimeSnapshot,
		execCtx.WorkDir,
		execCtx.RunID.String(),
		accountID,
		execCtx.ProfileRef,
		execCtx.WorkspaceRef,
	)

	// extract raw output text; try "output" then "stdout", fall back to full JSON
	content := raw
	for _, key := range []string{"output", "stdout"} {
		if out, ok := result.ResultJSON[key]; ok {
			if v, ok := out.(string); ok {
				content = []byte(v)
				break
			}
		}
	}

	threadID := ""
	if execCtx.ThreadID != nil {
		threadID = execCtx.ThreadID.String()
	} else {
		threadID = execCtx.RunID.String()
	}
	filePath := filepath.Join(".tool-outputs", threadID, toolCallID+".txt")
	if writeErr := backend.WriteFile(ctx, filePath, content); writeErr != nil {
		slog.Warn("persist_large_result: write failed, falling back to compression",
			"run_id", execCtx.RunID.String(),
			"tool_call_id", toolCallID,
			"filepath", filePath,
			"error", writeErr.Error(),
		)
		return result
	}

	preview := generatePreview(content, PersistPreviewBytes)
	originalBytes := len(content)

	newResult := make(map[string]any, len(metadataFields)+5)
	for k, v := range result.ResultJSON {
		if _, keep := metadataFields[k]; keep {
			newResult[k] = v
		}
	}
	newResult["persisted"] = true
	newResult["filepath"] = filePath
	newResult["original_bytes"] = originalBytes
	newResult["preview"] = preview
	newResult["hint"] = "Full output saved. Read the file at the given path to access the complete content."

	return ExecutionResult{
		ResultJSON:   newResult,
		ContentParts: result.ContentParts,
		Error:        result.Error,
		DurationMs:   result.DurationMs,
		Usage:        result.Usage,
		Events:       result.Events,
	}
}

func generatePreview(raw []byte, budget int) string {
	if len(raw) <= budget {
		return string(raw)
	}
	cut := make([]byte, budget)
	copy(cut, raw[:budget])
	if idx := bytes.LastIndexByte(cut, '\n'); idx > budget/2 {
		cut = cut[:idx]
	}
	return string(cut) + "\n...[truncated]"
}

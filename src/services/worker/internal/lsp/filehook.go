package lsp

import (
	"context"
	"log/slog"
	"path/filepath"
	"time"

	"arkloop/services/worker/internal/tools"
)

type LSPAwareExecutor struct {
	inner   tools.Executor
	manager *Manager
	logger  *slog.Logger
}

func NewLSPAwareExecutor(inner tools.Executor, manager *Manager, logger *slog.Logger) *LSPAwareExecutor {
	return &LSPAwareExecutor{
		inner:   inner,
		manager: manager,
		logger:  logger,
	}
}

func (e *LSPAwareExecutor) Execute(
	ctx context.Context,
	toolName string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	toolCallID string,
) tools.ExecutionResult {
	result := e.inner.Execute(ctx, toolName, args, execCtx, toolCallID)

	if isFileWriteTool(toolName) && result.Error == nil {
		path, _ := args["file_path"].(string)
		if path != "" {
			absPath := resolvePath(execCtx.WorkDir, path)
			uri := PathToURI(absPath)

			// clear stale diagnostics before notifying, so new diagnostics
			// from the LSP won't be immediately wiped
			e.manager.DiagRegistry().MarkFileEdited(uri)
			e.manager.DiagRegistry().ClearFile(uri)

			hookCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if err := e.manager.NotifyFileChanged(hookCtx, absPath); err != nil {
				e.logger.Warn("lsp notify file changed failed", "path", absPath, "err", err)
			}
		}
	}

	return result
}

func isFileWriteTool(name string) bool {
	return name == "edit" || name == "write_file"
}

func resolvePath(workDir, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(workDir, path)
}

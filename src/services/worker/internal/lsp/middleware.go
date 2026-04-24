//go:build desktop

package lsp

import (
	"context"
	"time"

	"arkloop/services/worker/internal/pipeline"
)

// NewDiagnosticMiddleware syncs the LSP root directory from RunContext.WorkDir
// and injects active LSP diagnostics into the system prompt.
func NewDiagnosticMiddleware(manager *Manager) pipeline.RunMiddleware {
	return func(ctx context.Context, rc *pipeline.RunContext, next pipeline.RunHandler) error {
		if manager == nil {
			return next(ctx, rc)
		}
		if rc.WorkDir != "" && rc.WorkDir != manager.RootDir() {
			manager.SetRootDir(rc.WorkDir)
		}
		if manager.DiagRegistry().HasRecentEdits() {
			time.Sleep(ActiveWaitDelay)
		}
		diags := manager.GetDiagnostics()
		if diags != "" {
			rc.PromptAssembly.Upsert(pipeline.PromptSegment{
				Name:          "lsp_diagnostics",
				Target:        pipeline.PromptTargetRuntimeTail,
				Text:          diags,
				Stability:     pipeline.PromptStabilityVolatileTail,
				CacheEligible: false,
			})
		}
		return next(ctx, rc)
	}
}

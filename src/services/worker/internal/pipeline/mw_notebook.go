//go:build !desktop

package pipeline

import (
	"context"
	"log/slog"
	"strings"

	notebookprovider "arkloop/services/worker/internal/memory/notebook"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewNotebookInjectionMiddleware injects the cached <notebook> block
// into prompt assembly before the run, independently of the semantic
// memory snapshot.
func NewNotebookInjectionMiddleware(pool *pgxpool.Pool) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if pool == nil || rc.UserID == nil {
			return next(ctx, rc)
		}
		rc.RemovePromptSegment("memory.notebook_snapshot")
		rc.RemovePromptSegment("memory.notebook_unavailable")
		provider := notebookprovider.NewProvider(pool)
		block, err := provider.GetSnapshot(ctx, rc.Run.AccountID, *rc.UserID, StableAgentID(rc))
		if err != nil {
			slog.ErrorContext(ctx, "notebook: snapshot read failed", "err", err.Error())
			appendAsyncRunEvent(ctx, rc.MemoryServiceDB, rc.Run.ID, rc.Emitter.Emit("notebook.snapshot.read_failed", map[string]any{
				"message": err.Error(),
			}, nil, nil))
			rc.UpsertPromptSegment(PromptSegment{
				Name:          "memory.notebook_unavailable",
				Target:        PromptTargetSystemPrefix,
				Role:          "system",
				Text:          "<notebook_unavailable>Notebook system temporarily unavailable. Proceed without notebook context.</notebook_unavailable>",
				Stability:     PromptStabilitySessionPrefix,
				CacheEligible: true,
			})
			return next(ctx, rc)
		}
		if strings.TrimSpace(block) != "" {
			rc.UpsertPromptSegment(PromptSegment{
				Name:          "memory.notebook_snapshot",
				Target:        PromptTargetSystemPrefix,
				Role:          "system",
				Text:          block,
				Stability:     PromptStabilitySessionPrefix,
				CacheEligible: true,
			})
		}
		return next(ctx, rc)
	}
}

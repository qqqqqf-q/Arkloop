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
// into rc.SystemPrompt before the run, independently of the OpenViking
// memory snapshot.
func NewNotebookInjectionMiddleware(pool *pgxpool.Pool) RunMiddleware {
	return func(ctx context.Context, rc *RunContext, next RunHandler) error {
		if pool == nil || rc.UserID == nil {
			return next(ctx, rc)
		}
		provider := notebookprovider.NewProvider(pool)
		block, err := provider.GetSnapshot(ctx, rc.Run.AccountID, *rc.UserID, StableAgentID(rc))
		if err != nil {
			slog.WarnContext(ctx, "notebook: snapshot read failed", "err", err.Error())
			return next(ctx, rc)
		}
		if strings.TrimSpace(block) != "" {
			rc.SystemPrompt += block
		}
		return next(ctx, rc)
	}
}

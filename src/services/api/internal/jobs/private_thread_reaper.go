package jobs

import (
	"context"
	"log/slog"
	"time"

	"arkloop/services/api/internal/data"
)

const privateReaperInterval = time.Hour

// PrivateThreadReaper 定期硬删除已过期的私密 thread。
type PrivateThreadReaper struct {
	threadRepo *data.ThreadRepository
	logger     *slog.Logger
}

func NewPrivateThreadReaper(
	threadRepo *data.ThreadRepository,
	logger *slog.Logger,
) *PrivateThreadReaper {
	return &PrivateThreadReaper{
		threadRepo: threadRepo,
		logger:     logger,
	}
}

func (r *PrivateThreadReaper) Run(ctx context.Context) {
	r.reap(ctx)

	ticker := time.NewTicker(privateReaperInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.reap(ctx)
		}
	}
}

func (r *PrivateThreadReaper) reap(ctx context.Context) {
	count, err := r.threadRepo.DeleteExpiredPrivate(ctx)
	if err != nil {
		r.logger.Error("private thread reap failed", "error", err.Error())
		return
	}
	if count > 0 {
		r.logger.Info("private threads reaped", "count", count)
	}
}

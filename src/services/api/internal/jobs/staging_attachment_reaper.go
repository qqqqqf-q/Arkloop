package jobs

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"arkloop/services/shared/objectstore"
)

const (
	stagingReapInterval  = 10 * time.Minute
	stagingMaxAge        = time.Hour
	stagingPrefix        = "attachments/"
	stagingMetaCreatedAt = "created_at"
)

type stagingAttachmentStore interface {
	ListPrefix(ctx context.Context, prefix string) ([]objectstore.ObjectInfo, error)
	Head(ctx context.Context, key string) (objectstore.ObjectInfo, error)
	Delete(ctx context.Context, key string) error
}

type StagingAttachmentReaper struct {
	store  stagingAttachmentStore
	logger *slog.Logger
}

func NewStagingAttachmentReaper(store stagingAttachmentStore, logger *slog.Logger) *StagingAttachmentReaper {
	return &StagingAttachmentReaper{store: store, logger: logger}
}

func (r *StagingAttachmentReaper) Run(ctx context.Context) {
	r.reap(ctx)
	ticker := time.NewTicker(stagingReapInterval)
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

func (r *StagingAttachmentReaper) reap(ctx context.Context) {
	objects, err := r.store.ListPrefix(ctx, stagingPrefix)
	if err != nil {
		r.logger.Error("staging reap: list failed", "error", err.Error())
		return
	}

	cutoff := time.Now().Unix() - int64(stagingMaxAge.Seconds())
	var deleted int
	for _, obj := range objects {
		info, err := r.store.Head(ctx, obj.Key)
		if err != nil {
			continue
		}
		createdAtStr := strings.TrimSpace(info.Metadata[stagingMetaCreatedAt])
		if createdAtStr == "" {
			continue
		}
		createdAt, err := strconv.ParseInt(createdAtStr, 10, 64)
		if err != nil {
			continue
		}
		if createdAt > cutoff {
			continue
		}
		if err := r.store.Delete(ctx, obj.Key); err != nil {
			r.logger.Warn("staging reap: delete failed", "key", obj.Key, "error", err.Error())
			continue
		}
		deleted++
	}
	if deleted > 0 {
		r.logger.Info("staging attachments reaped", "count", deleted)
	}
}

package featuregate

import (
	"context"
	nethttp "net/http"

	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/featureflag"
	httpkit "arkloop/services/api/internal/http/httpkit"

	"github.com/google/uuid"
)

type ThreadGetter interface {
	GetByID(ctx context.Context, threadID uuid.UUID) (*data.Thread, error)
}

func EnsureClawEnabled(
	w nethttp.ResponseWriter,
	traceID string,
	ctx context.Context,
	flagService *featureflag.Service,
) bool {
	if featureflag.IsClawEnabled(ctx, flagService) {
		return true
	}
	httpkit.WriteError(
		w,
		nethttp.StatusForbidden,
		"feature_flags.claw_disabled",
		"claw is disabled",
		traceID,
		map[string]any{"flag_key": featureflag.ClawEnabledKey},
	)
	return false
}

func EnsureClawEnabledForThread(
	w nethttp.ResponseWriter,
	traceID string,
	ctx context.Context,
	thread *data.Thread,
	flagService *featureflag.Service,
) bool {
	return EnsureClawEnabled(w, traceID, ctx, flagService)
}

func EnsureClawEnabledForRun(
	w nethttp.ResponseWriter,
	traceID string,
	ctx context.Context,
	run *data.Run,
	threadRepo ThreadGetter,
	flagService *featureflag.Service,
) bool {
	if run == nil || threadRepo == nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return false
	}
	thread, err := threadRepo.GetByID(ctx, run.ThreadID)
	if err != nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return false
	}
	if thread == nil {
		httpkit.WriteError(w, nethttp.StatusInternalServerError, "internal.error", "internal error", traceID, nil)
		return false
	}
	return EnsureClawEnabledForThread(w, traceID, ctx, thread, flagService)
}

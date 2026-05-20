package pipeline

import (
	"context"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	sharedconfig "arkloop/services/shared/config"

	"github.com/google/uuid"
)

// SuggestionRefreshFunc 创建 thread + run + 入队 job 以触发 suggestion 生成。
type SuggestionRefreshFunc func(ctx context.Context, accountID, userID uuid.UUID, agentID, mode string)

func addSuggestionScore(ctx context.Context, store SuggestionStore, accountID, userID uuid.UUID, agentID, mode string, delta int, resolver sharedconfig.Resolver, refresh SuggestionRefreshFunc) {
	if store == nil || delta <= 0 || mode == "" {
		return
	}
	newScore, lastBuildAt, err := store.AddScore(ctx, accountID, userID, agentID, mode, delta)
	if err != nil {
		slog.WarnContext(ctx, "suggestion: add score failed", "err", err.Error())
		return
	}

	// staleness bonus: 3 * ln(1 + hours_since_last_build)
	if lastBuildAt != nil {
		hours := time.Since(*lastBuildAt).Hours()
		if hours > 0 {
			staleness := int(3.0 * math.Log(1.0+hours))
			newScore += staleness
		}
	}

	threshold := resolveSuggestionThreshold(resolver)
	if newScore >= threshold {
		if err := store.ResetScore(ctx, accountID, userID, agentID, mode); err != nil {
			slog.WarnContext(ctx, "suggestion: reset score failed", "err", err.Error())
			return
		}
		if refresh != nil {
			refresh(ctx, accountID, userID, agentID, mode)
		}
	}
}

func resolveSuggestionThreshold(resolver sharedconfig.Resolver) int {
	if resolver == nil {
		return 15
	}
	raw, err := resolver.Resolve(context.Background(), "suggestion.score_threshold", sharedconfig.Scope{})
	if err != nil || strings.TrimSpace(raw) == "" {
		return 15
	}
	v, parseErr := strconv.Atoi(strings.TrimSpace(raw))
	if parseErr != nil || v <= 0 {
		return 15
	}
	return v
}

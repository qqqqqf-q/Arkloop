//go:build !desktop

package accountapi

import "context"

func appendLocalTelegramSelectorCandidates(ctx context.Context, candidates []telegramSelectorCandidate) []telegramSelectorCandidate {
	_ = ctx
	return candidates
}

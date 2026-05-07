//go:build desktop

package accountapi

import (
	"context"

	"arkloop/services/shared/localproviders"

	"github.com/google/uuid"
)

func appendLocalTelegramSelectorCandidates(ctx context.Context, candidates []telegramSelectorCandidate) []telegramSelectorCandidate {
	statuses := localproviders.NewResolver(localproviders.Options{}).ProviderStatuses(ctx)
	return appendLocalTelegramSelectorStatusCandidates(candidates, statuses)
}

func appendLocalTelegramSelectorStatusCandidates(candidates []telegramSelectorCandidate, statuses []localproviders.ProviderStatus) []telegramSelectorCandidate {
	for _, status := range statuses {
		credentialID := localTelegramProviderUUID(status.ID)
		if credentialID == uuid.Nil {
			continue
		}
		for _, model := range status.Models {
			if model.Hidden {
				continue
			}
			candidates = append(candidates, telegramSelectorCandidate{
				credentialID:   credentialID,
				credentialName: status.DisplayName,
				ownerKind:      "platform",
				model:          model.ID,
				priority:       model.Priority,
			})
		}
	}
	return candidates
}

func localTelegramProviderUUID(providerID string) uuid.UUID {
	switch providerID {
	case localproviders.ClaudeCodeProviderID, localproviders.CodexProviderID:
		return uuid.NewSHA1(uuid.NameSpaceURL, []byte("arkloop:local-provider:"+providerID))
	default:
		return uuid.Nil
	}
}

package conversationapi

import (
	"context"

	"arkloop/services/api/internal/data"

	"github.com/google/uuid"
)

func deleteThreadAttachmentObjects(ctx context.Context, store messageAttachmentStore, messageRepo *data.MessageRepository, accountID, threadID uuid.UUID) {
	if store == nil || messageRepo == nil || accountID == uuid.Nil || threadID == uuid.Nil {
		return
	}
	keys, err := messageRepo.ListAllAttachmentKeysByThread(ctx, accountID, threadID)
	if err != nil {
		return
	}
	for _, key := range keys {
		_ = store.Delete(ctx, key)
	}
}

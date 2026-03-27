package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"arkloop/services/shared/messagecontent"
	"arkloop/services/shared/objectstore"
	"arkloop/services/worker/internal/llm"

	"github.com/google/uuid"
)

const responseDraftKeyPrefix = "response_draft/run/"

type responseDraft struct {
	RunID     string `json:"run_id"`
	ThreadID  string `json:"thread_id"`
	Content   string `json:"content"`
	UpdatedAt string `json:"updated_at"`
	LastSeq   int64  `json:"last_seq"`
}

func responseDraftKey(runID uuid.UUID) string {
	return responseDraftKeyPrefix + runID.String() + ".json"
}

func WriteResponseDraft(ctx context.Context, store objectstore.BlobStore, runID uuid.UUID, threadID uuid.UUID, content string, lastSeq int64) error {
	if store == nil || runID == uuid.Nil || threadID == uuid.Nil {
		return nil
	}
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return DeleteResponseDraft(ctx, store, runID)
	}
	return store.WriteJSONAtomic(ctx, responseDraftKey(runID), responseDraft{
		RunID:     runID.String(),
		ThreadID:  threadID.String(),
		Content:   content,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		LastSeq:   lastSeq,
	})
}

func readResponseDraft(ctx context.Context, store objectstore.BlobStore, runID uuid.UUID) (*responseDraft, error) {
	if store == nil || runID == uuid.Nil {
		return nil, nil
	}
	data, err := store.Get(ctx, responseDraftKey(runID))
	if err != nil {
		if objectstore.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var draft responseDraft
	if err := json.Unmarshal(data, &draft); err != nil {
		return nil, fmt.Errorf("unmarshal response draft: %w", err)
	}
	if strings.TrimSpace(draft.Content) == "" {
		return nil, nil
	}
	return &draft, nil
}

func DeleteResponseDraft(ctx context.Context, store objectstore.BlobStore, runID uuid.UUID) error {
	if store == nil || runID == uuid.Nil {
		return nil
	}
	err := store.Delete(ctx, responseDraftKey(runID))
	if err != nil && !objectstore.IsNotFound(err) {
		return err
	}
	return nil
}

func responseDraftMessage(content string) llm.Message {
	return llm.Message{
		Role: "assistant",
		Content: []llm.ContentPart{{
			Type: messagecontent.PartTypeText,
			Text: content,
		}},
	}
}

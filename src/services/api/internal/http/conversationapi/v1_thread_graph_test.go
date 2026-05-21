package conversationapi

import (
	"testing"
	"time"

	"arkloop/services/api/internal/data"

	"github.com/google/uuid"
)

func TestBuildThreadGraphResponseMergesForkHistory(t *testing.T) {
	accountID := uuid.New()
	rootThreadID := uuid.New()
	forkThreadID := uuid.New()
	rootUserID := uuid.New()
	rootAssistantID := uuid.New()
	forkUserCopyID := uuid.New()
	forkAssistantCopyID := uuid.New()
	forkAssistantID := uuid.New()
	now := time.Now().UTC()

	threads := []data.Thread{
		{
			ID:        rootThreadID,
			AccountID: accountID,
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:                    forkThreadID,
			AccountID:             accountID,
			CreatedAt:             now.Add(time.Second),
			UpdatedAt:             now.Add(time.Second),
			ParentThreadID:        &rootThreadID,
			BranchedFromMessageID: &rootAssistantID,
		},
	}
	messages := []data.Message{
		messageForGraph(rootUserID, accountID, rootThreadID, 1, "user", "root question"),
		messageForGraph(rootAssistantID, accountID, rootThreadID, 7, "assistant", "root answer"),
		messageForGraph(forkUserCopyID, accountID, forkThreadID, 1, "user", "root question"),
		messageForGraph(forkAssistantCopyID, accountID, forkThreadID, 2, "assistant", "root answer"),
		messageForGraph(forkAssistantID, accountID, forkThreadID, 3, "assistant", "retry answer"),
	}

	resp := buildThreadGraphResponse(forkThreadID, threads, messages)

	if resp.RootThreadID != rootThreadID.String() {
		t.Fatalf("root_thread_id = %s, want %s", resp.RootThreadID, rootThreadID)
	}
	if len(resp.Messages) != 3 {
		t.Fatalf("messages len = %d, want 3", len(resp.Messages))
	}
	if got := len(resp.Messages[0].Instances); got != 2 {
		t.Fatalf("root user instances = %d, want 2", got)
	}
	if got := len(resp.Messages[1].Instances); got != 2 {
		t.Fatalf("root assistant instances = %d, want 2", got)
	}
	if resp.Messages[2].Message.ID != forkAssistantID.String() {
		t.Fatalf("fork node message id = %s, want %s", resp.Messages[2].Message.ID, forkAssistantID)
	}
	if resp.Messages[2].ParentGraphNodeID == nil || *resp.Messages[2].ParentGraphNodeID != resp.Messages[1].GraphNodeID {
		t.Fatalf("fork node parent = %#v, want %s", resp.Messages[2].ParentGraphNodeID, resp.Messages[1].GraphNodeID)
	}
	if len(resp.Edges) != 2 {
		t.Fatalf("edges len = %d, want 2", len(resp.Edges))
	}
}

func messageForGraph(id, accountID, threadID uuid.UUID, seq int64, role, content string) data.Message {
	return data.Message{
		ID:        id,
		AccountID: accountID,
		ThreadID:  threadID,
		ThreadSeq: seq,
		Role:      role,
		Content:   content,
		CreatedAt: time.Unix(seq, 0).UTC(),
	}
}

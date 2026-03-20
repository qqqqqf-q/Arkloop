package pipeline

import (
	"strings"
	"testing"

	"arkloop/services/worker/internal/llm"

	"github.com/google/uuid"
)

func TestCompactThreadMessages_trimCount(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: "a"}}},
		{Role: "assistant", Content: []llm.TextPart{{Text: "b"}}},
		{Role: "user", Content: []llm.TextPart{{Text: "c"}}},
	}
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	cfg := ContextCompactSettings{Enabled: true, MaxMessages: 2}
	out, outIDs, dropped := CompactThreadMessages(msgs, ids, cfg, nil)
	if dropped != 1 || len(out) != 2 {
		t.Fatalf("expected drop 1, len 2, got dropped=%d len=%d", dropped, len(out))
	}
	if out[0].Role != "assistant" || outIDs[0] != ids[1] {
		t.Fatalf("unexpected suffix start: %#v", out[0].Role)
	}
}

func TestCompactThreadMessages_userTokenBudget(t *testing.T) {
	long := strings.Repeat("a", 600) // 150 近似 tokens，与尾部 user 合计超预算
	msgs := []llm.Message{
		{Role: "user", Content: []llm.TextPart{{Text: long}}},
		{Role: "assistant", Content: []llm.TextPart{{Text: "ok"}}},
		{Role: "user", Content: []llm.TextPart{{Text: "tail"}}},
	}
	cfg := ContextCompactSettings{Enabled: true, MaxUserMessageTokens: 120}
	out, _, dropped := CompactThreadMessages(msgs, nil, cfg, nil)
	if dropped == 0 || len(out) == len(msgs) {
		t.Fatalf("expected head dropped, dropped=%d len=%d", dropped, len(out))
	}
	if out[len(out)-1].Role != "user" {
		t.Fatal("expected tail preserved")
	}
}

func TestContextCompactHasActiveBudget(t *testing.T) {
	if ContextCompactHasActiveBudget(ContextCompactSettings{Enabled: true}) {
		t.Fatal("expected false when all budgets zero")
	}
	if !ContextCompactHasActiveBudget(ContextCompactSettings{Enabled: true, MaxMessages: 1}) {
		t.Fatal("expected true")
	}
}

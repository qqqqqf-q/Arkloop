package pipeline

import (
	"testing"

	"arkloop/services/worker/internal/data"
)

func TestResolveContextSeqRangeForReplacementPrefersCurrentThreadGraph(t *testing.T) {
	chunks := []canonicalChunk{
		{ContextSeq: 1, StartThreadSeq: 100, EndThreadSeq: 100},
		{ContextSeq: 2, StartThreadSeq: 101, EndThreadSeq: 101},
		{ContextSeq: 3, StartThreadSeq: 102, EndThreadSeq: 102},
	}
	replacement := data.ThreadContextReplacementRecord{
		StartThreadSeq:  100,
		EndThreadSeq:    101,
		StartContextSeq: 40,
		EndContextSeq:   41,
	}

	start, end, ok := resolveContextSeqRangeForReplacement(chunks, replacement)
	if !ok {
		t.Fatal("expected replacement range to resolve")
	}
	if start != 1 || end != 2 {
		t.Fatalf("resolved range = %d-%d, want 1-2", start, end)
	}
}

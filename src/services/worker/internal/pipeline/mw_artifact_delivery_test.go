package pipeline

import (
	"testing"

	"arkloop/services/worker/internal/data"
)

func TestContainsArtifactReferenceOutputs(t *testing.T) {
	tests := []struct {
		name    string
		outputs []string
		want    bool
	}{
		{"doc link", []string{"see [Report](artifact:acct/run/r.md)"}, true},
		{"image", []string{"![chart](artifact:acct/c.png)"}, true},
		{"plain text", []string{"no artifacts here"}, false},
		{"bare ref not matched", []string{"artifact:acct/x"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsArtifactReferenceOutputs(tt.outputs); got != tt.want {
				t.Fatalf("containsArtifactReferenceOutputs = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSplitArtifactSegments(t *testing.T) {
	got := splitArtifactSegments("Here is the report [Report](artifact:acct/run/r.md) enjoy")
	want := []data.OutboxSegment{
		{Kind: "text", Text: "Here is the report"},
		{Kind: "artifact", ArtifactKey: "acct/run/r.md"},
		{Kind: "text", Text: "enjoy"},
	}
	assertSegmentsEqual(t, got, want)
}

func TestApplyArtifactDeliverySegments(t *testing.T) {
	t.Run("no artifact passthrough", func(t *testing.T) {
		outs := []string{"just text"}
		gotOuts, gotSegs := applyArtifactDeliverySegments(outs, nil)
		if len(gotSegs) != 0 {
			t.Fatalf("expected no segments, got %v", gotSegs)
		}
		if len(gotOuts) != 1 || gotOuts[0] != "just text" {
			t.Fatalf("outputs mutated: %v", gotOuts)
		}
	})

	t.Run("doc link without stickers", func(t *testing.T) {
		outs := []string{"Here is the report [Report](artifact:acct/run/r.md)"}
		gotOuts, gotSegs := applyArtifactDeliverySegments(outs, nil)
		assertSegmentsEqual(t, gotSegs, []data.OutboxSegment{
			{Kind: "text", Text: "Here is the report"},
			{Kind: "artifact", ArtifactKey: "acct/run/r.md"},
		})
		if len(gotOuts) != 1 || gotOuts[0] != "Here is the report" {
			t.Fatalf("clean outputs = %v", gotOuts)
		}
	})

	t.Run("image only yields no text", func(t *testing.T) {
		_, gotSegs := applyArtifactDeliverySegments([]string{"![chart](artifact:acct/c.png)"}, nil)
		assertSegmentsEqual(t, gotSegs, []data.OutboxSegment{
			{Kind: "artifact", ArtifactKey: "acct/c.png"},
		})
	})

	t.Run("interleaves with existing sticker segments", func(t *testing.T) {
		segs := []data.OutboxSegment{
			{Kind: "text", Text: "hi [Report](artifact:k)"},
			{Kind: "sticker", StickerID: "s1"},
		}
		_, gotSegs := applyArtifactDeliverySegments([]string{"hi"}, segs)
		assertSegmentsEqual(t, gotSegs, []data.OutboxSegment{
			{Kind: "text", Text: "hi"},
			{Kind: "artifact", ArtifactKey: "k"},
			{Kind: "sticker", StickerID: "s1"},
		})
	})
}

func assertSegmentsEqual(t *testing.T, got, want []data.OutboxSegment) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("segment count = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("segment[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

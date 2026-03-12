//go:build !desktop

package sandbox

import (
	"strings"
	"testing"
)

func TestParseBrowserSnapshotBuildsCompactSummary(t *testing.T) {
	raw := browserSnapshotJSON(
		"https://example.com",
		"Example Domain",
		"- heading \"Example Domain\" [ref=e1]\n- paragraph [ref=e2]: This domain is for use in illustrative examples.\n- link \"More information\" [ref=e3]\n- textbox \"Search\" [ref=e4]",
		map[string]any{
			"e1": map[string]any{"role": "heading", "text": "Example Domain"},
			"e3": map[string]any{"role": "link", "text": "More information"},
			"e4": map[string]any{"role": "textbox", "label": "Search"},
		},
	)
	payload, ok := parseBrowserSnapshot(raw)
	if !ok {
		t.Fatal("expected compact snapshot parse to succeed")
	}
	if payload.URL != "https://example.com" {
		t.Fatalf("unexpected url: %q", payload.URL)
	}
	if payload.Title != "Example Domain" {
		t.Fatalf("unexpected title: %q", payload.Title)
	}
	if len(payload.Clickables) != 1 || payload.Clickables[0].Ref != "e3" {
		t.Fatalf("unexpected clickables: %#v", payload.Clickables)
	}
	if len(payload.FormControls) != 1 || payload.FormControls[0].Ref != "e4" {
		t.Fatalf("unexpected form controls: %#v", payload.FormControls)
	}
	if len(payload.VisibleText) == 0 {
		t.Fatalf("expected visible text summary, got %#v", payload.VisibleText)
	}
	if !strings.Contains(payload.Output, "URL: https://example.com") || !strings.Contains(payload.Output, "Clickable:") {
		t.Fatalf("unexpected compact output: %q", payload.Output)
	}
}

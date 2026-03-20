package memory

import "testing"

func TestSearchScopeForDesktopLocal(t *testing.T) {
	if got := SearchScopeForDesktopLocal(map[string]any{}); got != MemoryScope("") {
		t.Fatalf("empty args = %q want empty", got)
	}
	if got := SearchScopeForDesktopLocal(map[string]any{"scope": ""}); got != MemoryScope("") {
		t.Fatalf("empty scope = %q want empty", got)
	}
	if got := SearchScopeForDesktopLocal(map[string]any{"scope": "user"}); got != MemoryScopeUser {
		t.Fatalf("user = %q", got)
	}
	if got := SearchScopeForDesktopLocal(map[string]any{"scope": "agent"}); got != MemoryScopeAgent {
		t.Fatalf("agent = %q", got)
	}
}

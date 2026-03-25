package memory

import "testing"

func TestSearchScopeForDesktopLocal(t *testing.T) {
	if got := SearchScopeForDesktopLocal(map[string]any{}); got != MemoryScopeUser {
		t.Fatalf("empty args = %q want user", got)
	}
	if got := SearchScopeForDesktopLocal(map[string]any{"scope": ""}); got != MemoryScopeUser {
		t.Fatalf("empty scope = %q want user", got)
	}
	if got := SearchScopeForDesktopLocal(map[string]any{"scope": "user"}); got != MemoryScopeUser {
		t.Fatalf("user = %q", got)
	}
	if got := SearchScopeForDesktopLocal(map[string]any{"scope": "agent"}); got != MemoryScopeUser {
		t.Fatalf("agent should normalize to user, got %q", got)
	}
}

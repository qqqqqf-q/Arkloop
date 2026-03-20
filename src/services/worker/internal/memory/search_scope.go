package memory

import "strings"

// SearchScopeForDesktopLocal interprets memory_search "scope" for Desktop SQLite backend.
// Missing or empty scope searches both user and agent rows; explicit "user" / "agent" filters.
func SearchScopeForDesktopLocal(args map[string]any) MemoryScope {
	s, ok := args["scope"].(string)
	if !ok || strings.TrimSpace(s) == "" {
		return MemoryScope("")
	}
	if strings.TrimSpace(s) == string(MemoryScopeAgent) {
		return MemoryScopeAgent
	}
	return MemoryScopeUser
}

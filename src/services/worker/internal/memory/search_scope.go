package memory

import "strings"

// SearchScopeForDesktopLocal interprets memory_search "scope" for Desktop SQLite backend.
// User is the only memory subject. Legacy/explicit "agent" is accepted but normalized to user.
func SearchScopeForDesktopLocal(args map[string]any) MemoryScope {
	s, ok := args["scope"].(string)
	if !ok || strings.TrimSpace(s) == "" {
		return MemoryScopeUser
	}
	// Keep accepting the old enum value, but route it to user scope.
	if strings.EqualFold(strings.TrimSpace(s), string(MemoryScopeAgent)) {
		return MemoryScopeUser
	}
	return MemoryScopeUser
}

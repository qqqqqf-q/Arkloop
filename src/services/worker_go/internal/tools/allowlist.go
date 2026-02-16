package tools

import (
	"os"
	"sort"
	"strings"
)

const toolAllowlistEnv = "ARKLOOP_TOOL_ALLOWLIST"

type Allowlist struct {
	allowed map[string]struct{}
}

func AllowlistFromNames(names []string) Allowlist {
	allowed := map[string]struct{}{}
	for _, name := range names {
		cleaned := strings.TrimSpace(name)
		if cleaned == "" {
			continue
		}
		allowed[cleaned] = struct{}{}
	}
	return Allowlist{allowed: allowed}
}

func (a Allowlist) Allows(toolName string) bool {
	_, ok := a.allowed[toolName]
	return ok
}

func (a Allowlist) ToSortedList() []string {
	names := make([]string, 0, len(a.allowed))
	for name := range a.allowed {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func ParseAllowlistNamesFromEnv() []string {
	raw := strings.TrimSpace(os.Getenv(toolAllowlistEnv))
	if raw == "" {
		return nil
	}

	items := strings.Split(raw, ",")
	deduped := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		cleaned := strings.TrimSpace(item)
		if cleaned == "" {
			continue
		}
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		deduped = append(deduped, cleaned)
	}
	return deduped
}


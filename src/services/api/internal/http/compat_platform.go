package http

import (
	"strings"

	sharedconfig "arkloop/services/shared/config"
)

const maskedSensitiveValue = "******"

type notificationResponse struct {
	ID          string         `json:"id"`
	UserID      string         `json:"user_id"`
	OrgID       string         `json:"org_id"`
	Type        string         `json:"type"`
	Title       string         `json:"title"`
	Body        string         `json:"body"`
	PayloadJSON map[string]any `json:"payload"`
	ReadAt      *string        `json:"read_at,omitempty"`
	CreatedAt   string         `json:"created_at"`
}

func maskIfSensitive(key, value string, registry *sharedconfig.Registry) string {
	if registry == nil {
		registry = sharedconfig.DefaultRegistry()
	}
	entry, ok := registry.Get(key)
	if !ok || !entry.Sensitive {
		return value
	}
	if strings.TrimSpace(value) == "" {
		return value
	}
	return maskedSensitiveValue
}

func filterSchemaEntries(entries []sharedconfig.Entry, isPlatformAdmin bool) []sharedconfig.Entry {
	if isPlatformAdmin {
		return entries
	}

	out := make([]sharedconfig.Entry, 0, len(entries))
	for _, e := range entries {
		if e.Scope == sharedconfig.ScopeProject || e.Scope == sharedconfig.ScopeBoth {
			out = append(out, e)
		}
	}
	return out
}

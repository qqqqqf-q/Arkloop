package llm

import (
	"fmt"
	"regexp"
	"strings"
)

const debugPayloadRedactMinChars = 80

var debugPayloadDataURLPattern = regexp.MustCompile(`data:([a-zA-Z0-9.+-]+/[a-zA-Z0-9.+-]+);base64,([A-Za-z0-9+/=\s\r\n]+)`)

type debugPayloadRedactionStats struct {
	DataURLCount     int
	Base64FieldCount int
}

func sanitizeDebugPayloadJSON(payload map[string]any) (map[string]any, map[string]any) {
	if payload == nil {
		return map[string]any{}, nil
	}
	stats := &debugPayloadRedactionStats{}
	sanitized, _ := sanitizeDebugPayloadValue(payload, "", nil, stats).(map[string]any)
	if sanitized == nil {
		sanitized = map[string]any{}
	}
	hints := map[string]any{}
	if stats.DataURLCount > 0 {
		hints["data_url_redactions"] = stats.DataURLCount
	}
	if stats.Base64FieldCount > 0 {
		hints["base64_field_redactions"] = stats.Base64FieldCount
	}
	if len(hints) == 0 {
		return sanitized, nil
	}
	return sanitized, hints
}

func sanitizeDebugPayloadValue(value any, key string, parent map[string]any, stats *debugPayloadRedactionStats) any {
	switch typed := value.(type) {
	case map[string]any:
		cloned := make(map[string]any, len(typed))
		for childKey, childValue := range typed {
			cloned[childKey] = sanitizeDebugPayloadValue(childValue, childKey, typed, stats)
		}
		return cloned
	case []any:
		cloned := make([]any, 0, len(typed))
		for _, item := range typed {
			cloned = append(cloned, sanitizeDebugPayloadValue(item, "", nil, stats))
		}
		return cloned
	case []map[string]any:
		cloned := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			sanitized, _ := sanitizeDebugPayloadValue(item, "", nil, stats).(map[string]any)
			cloned = append(cloned, sanitized)
		}
		return cloned
	case string:
		if redacted, ok := redactDebugDataURL(typed); ok {
			stats.DataURLCount++
			return redacted
		}
		if shouldRedactBase64Field(key, parent, typed) {
			stats.Base64FieldCount++
			return formatBase64Redaction(typed)
		}
		return typed
	default:
		return value
	}
}

func redactDebugDataURL(value string) (string, bool) {
	redacted := false
	out := debugPayloadDataURLPattern.ReplaceAllStringFunc(value, func(match string) string {
		submatches := debugPayloadDataURLPattern.FindStringSubmatch(match)
		if len(submatches) != 3 {
			return match
		}
		compact := stripWhitespace(submatches[2])
		if len(compact) < debugPayloadRedactMinChars {
			return match
		}
		redacted = true
		return fmt.Sprintf("[data:%s;base64 redacted ~%d chars]", submatches[1], len(compact))
	})
	return out, redacted
}

func shouldRedactBase64Field(key string, parent map[string]any, value string) bool {
	if !strings.EqualFold(strings.TrimSpace(key), "data") {
		return false
	}
	if parent == nil || !looksLikeBase64Blob(value) {
		return false
	}
	if typ, _ := parent["type"].(string); strings.EqualFold(strings.TrimSpace(typ), "base64") {
		return true
	}
	return hasNonEmptyStringKey(parent, "media_type", "mime_type", "mimeType")
}

func hasNonEmptyStringKey(m map[string]any, keys ...string) bool {
	for _, key := range keys {
		value, _ := m[key].(string)
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func looksLikeBase64Blob(value string) bool {
	compact := stripWhitespace(value)
	if len(compact) < debugPayloadRedactMinChars {
		return false
	}
	for _, r := range compact {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '+', r == '/', r == '=':
		default:
			return false
		}
	}
	return true
}

func formatBase64Redaction(value string) string {
	return fmt.Sprintf("[base64 redacted ~%d chars]", len(stripWhitespace(value)))
}

func stripWhitespace(value string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
			return -1
		}
		return r
	}, value)
}

package llm

import (
	"encoding/json"
	"strings"

	"arkloop/services/worker/internal/stablejson"
)

var canonicalToolNameAliases = map[string]string{
	"read.minimax":        "read",
	"web_fetch.basic":     "web_fetch",
	"web_fetch.firecrawl": "web_fetch",
	"web_fetch.jina":      "web_fetch",
	"web_search.basic":    "web_search",
	"web_search.exa":      "web_search",
	"web_search.searxng":  "web_search",
	"web_search.tavily":   "web_search",
}

func CanonicalToolName(raw string) string {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		return ""
	}
	if mapped, ok := canonicalToolNameAliases[strings.ToLower(cleaned)]; ok {
		return mapped
	}
	return cleaned
}

func CanonicalToolCall(call ToolCall) ToolCall {
	call.ToolName = CanonicalToolName(call.ToolName)
	if call.ArgumentsJSON == nil {
		call.ArgumentsJSON = map[string]any{}
	}
	if raw, ok := call.ArgumentsJSON["display_description"]; ok {
		if s, ok := raw.(string); ok && strings.TrimSpace(s) != "" {
			call.DisplayDescription = strings.TrimSpace(s)
		}
		delete(call.ArgumentsJSON, "display_description")
	}
	return call
}

func toolCallArgumentsForModel(call ToolCall) map[string]any {
	args := make(map[string]any, len(call.ArgumentsJSON)+1)
	for key, value := range call.ArgumentsJSON {
		if key == "display_description" {
			if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
				args[key] = strings.TrimSpace(s)
			}
			continue
		}
		args[key] = value
	}
	if displayDescription := strings.TrimSpace(call.DisplayDescription); displayDescription != "" {
		args["display_description"] = displayDescription
	}
	return args
}

func CanonicalToolCalls(calls []ToolCall) []ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := append([]ToolCall(nil), calls...)
	for i := range out {
		out[i] = CanonicalToolCall(out[i])
	}
	return out
}

func CanonicalizeToolEnvelopeText(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return text
	}

	var envelope map[string]any
	if err := json.Unmarshal([]byte(trimmed), &envelope); err != nil {
		return text
	}

	rawName, ok := envelope["tool_name"].(string)
	if !ok {
		return text
	}
	canonical := CanonicalToolName(rawName)
	if canonical == "" || canonical == strings.TrimSpace(rawName) {
		return text
	}
	envelope["tool_name"] = canonical

	encoded, err := stablejson.Encode(envelope)
	if err == nil {
		return encoded
	}
	fallback, err := json.Marshal(envelope)
	if err != nil {
		return text
	}
	return string(fallback)
}

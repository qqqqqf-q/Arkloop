package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

func geminiErrorMessageAndDetails(body []byte, status int, bodyTruncated bool) (string, map[string]any) {
	details := map[string]any{"status_code": status}
	if len(body) > 0 {
		raw, truncated := truncateUTF8(string(body), geminiMaxErrorBodyBytes)
		details["provider_error_body"] = raw
		if bodyTruncated || truncated {
			details["provider_error_body_truncated"] = true
		}
	}

	fallback := "Gemini request failed"
	if raw, ok := details["provider_error_body"].(string); ok && strings.TrimSpace(raw) != "" {
		fallback = fmt.Sprintf("Gemini request failed: status=%d body=%q", status, raw)
	}

	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return fallback, details
	}
	errObj, ok := root["error"].(map[string]any)
	if !ok {
		if msg, ok := root["message"].(string); ok && strings.TrimSpace(msg) != "" {
			return strings.TrimSpace(msg), details
		}
		return fallback, details
	}
	if code, ok := errObj["code"].(float64); ok {
		details["gemini_error_code"] = int(code)
	}
	if status, ok := errObj["status"].(string); ok && strings.TrimSpace(status) != "" {
		details["gemini_error_status"] = strings.TrimSpace(status)
	}
	if msg, ok := errObj["message"].(string); ok && strings.TrimSpace(msg) != "" {
		return strings.TrimSpace(msg), details
	}
	return fallback, details
}

type geminiGenerateContentResponse struct {
	Candidates    []geminiCandidate    `json:"candidates"`
	UsageMetadata *geminiUsageMetadata `json:"usageMetadata"`
}

type geminiCandidate struct {
	Content      geminiContent `json:"content"`
	FinishReason string        `json:"finishReason"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
	Role  string       `json:"role"`
}

type geminiPart struct {
	Text         string              `json:"text"`
	Thought      bool                `json:"thought"`
	FunctionCall *geminiFunctionCall `json:"functionCall"`
}

type geminiFunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type geminiUsageMetadata struct {
	PromptTokenCount        int `json:"promptTokenCount"`
	CandidatesTokenCount    int `json:"candidatesTokenCount"`
	TotalTokenCount         int `json:"totalTokenCount"`
	CachedContentTokenCount int `json:"cachedContentTokenCount"`
}

func parseGeminiUsage(meta *geminiUsageMetadata) *Usage {
	if meta == nil {
		return nil
	}
	u := &Usage{}
	if meta.PromptTokenCount > 0 {
		v := meta.PromptTokenCount
		u.InputTokens = &v
	}
	if meta.CandidatesTokenCount > 0 {
		v := meta.CandidatesTokenCount
		u.OutputTokens = &v
	}
	if meta.TotalTokenCount > 0 {
		v := meta.TotalTokenCount
		u.TotalTokens = &v
	}
	if meta.CachedContentTokenCount > 0 {
		v := meta.CachedContentTokenCount
		u.CachedTokens = &v
	}
	return u
}

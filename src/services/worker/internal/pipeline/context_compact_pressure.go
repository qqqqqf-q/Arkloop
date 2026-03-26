package pipeline

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"arkloop/services/worker/internal/llm"

	"github.com/google/uuid"
)

// ContextCompactPressureAnchor 表示最近一次真实 request 的上下文锚点。
type ContextCompactPressureAnchor struct {
	LastRealPromptTokens             int
	LastRequestContextEstimateTokens int
}

func (a ContextCompactPressureAnchor) Valid() bool {
	return a.LastRealPromptTokens > 0 && a.LastRequestContextEstimateTokens > 0
}

type ContextCompactPressureStats struct {
	ContextEstimateTokens            int
	ContextPressureTokens            int
	LastRealPromptTokens             int
	LastRequestContextEstimateTokens int
	Anchored                         bool
}

// ApplyContextCompactPressure 用 gemini-cli 风格口径估算当前上下文压力。
func ApplyContextCompactPressure(anchor ContextCompactPressureAnchor, currentEstimateTokens int) int {
	if currentEstimateTokens < 0 {
		currentEstimateTokens = 0
	}
	if !anchor.Valid() {
		return currentEstimateTokens
	}
	pressure := anchor.LastRealPromptTokens + (currentEstimateTokens - anchor.LastRequestContextEstimateTokens)
	if pressure < 0 {
		return 0
	}
	return pressure
}

func ComputeContextCompactPressure(currentEstimateTokens int, anchor *ContextCompactPressureAnchor) ContextCompactPressureStats {
	stats := ContextCompactPressureStats{
		ContextEstimateTokens: currentEstimateTokens,
		ContextPressureTokens: currentEstimateTokens,
	}
	if stats.ContextEstimateTokens < 0 {
		stats.ContextEstimateTokens = 0
	}
	if anchor == nil || !anchor.Valid() {
		if stats.ContextPressureTokens < 0 {
			stats.ContextPressureTokens = 0
		}
		return stats
	}
	stats.Anchored = true
	stats.LastRealPromptTokens = anchor.LastRealPromptTokens
	stats.LastRequestContextEstimateTokens = anchor.LastRequestContextEstimateTokens
	stats.ContextPressureTokens = ApplyContextCompactPressure(*anchor, currentEstimateTokens)
	return stats
}

func ApplyContextCompactPressureFields(payload map[string]any, stats ContextCompactPressureStats) {
	if payload == nil {
		return
	}
	payload["context_estimate_tokens"] = stats.ContextEstimateTokens
	payload["context_pressure_tokens"] = stats.ContextPressureTokens
	if stats.Anchored {
		payload["last_real_prompt_tokens"] = stats.LastRealPromptTokens
		payload["last_request_context_estimate_tokens"] = stats.LastRequestContextEstimateTokens
	}
}

func contextCompactRequestMessages(systemPrompt string, msgs []llm.Message) []llm.Message {
	requestMsgs := append([]llm.Message(nil), msgs...)
	if strings.TrimSpace(systemPrompt) == "" {
		return requestMsgs
	}
	return append([]llm.Message{{
		Role:    "system",
		Content: []llm.TextPart{{Text: systemPrompt}},
	}}, requestMsgs...)
}

func latestContextCompactPressureAnchor(
	ctx context.Context,
	pool CompactPersistDB,
	accountID,
	threadID uuid.UUID,
) *ContextCompactPressureAnchor {
	if pool == nil || accountID == uuid.Nil || threadID == uuid.Nil {
		return nil
	}
	var raw []byte
	err := pool.QueryRow(ctx,
		`SELECT re.data_json
		   FROM run_events re
		   JOIN runs r ON r.id = re.run_id
		  WHERE r.account_id = $1
		    AND r.thread_id = $2
		    AND re.type = 'llm.turn.completed'
		  ORDER BY re.ts DESC, re.seq DESC
		  LIMIT 1`,
		accountID,
		threadID,
	).Scan(&raw)
	if err != nil || len(raw) == 0 {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	lastRealPromptTokens, ok := contextCompactAnyToInt64(payload["last_real_prompt_tokens"])
	if !ok || lastRealPromptTokens <= 0 {
		return nil
	}
	lastRequestEstimateTokens, ok := contextCompactAnyToInt64(payload["last_request_context_estimate_tokens"])
	if !ok || lastRequestEstimateTokens <= 0 {
		return nil
	}
	anchor := &ContextCompactPressureAnchor{
		LastRealPromptTokens:             int(lastRealPromptTokens),
		LastRequestContextEstimateTokens: int(lastRequestEstimateTokens),
	}
	if !anchor.Valid() {
		return nil
	}
	return anchor
}

func contextCompactAnyToInt64(v any) (int64, bool) {
	switch typed := v.(type) {
	case int:
		return int64(typed), true
	case int32:
		return int64(typed), true
	case int64:
		return typed, true
	case float64:
		return int64(typed), true
	case json.Number:
		n, err := typed.Int64()
		if err == nil {
			return n, true
		}
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err == nil {
			return n, true
		}
	}
	return 0, false
}

func resolveContextCompactPressureAnchor(
	ctx context.Context,
	pool CompactPersistDB,
	rc *RunContext,
) (ContextCompactPressureAnchor, bool) {
	if rc != nil && rc.HasContextCompactAnchor {
		anchor := ContextCompactPressureAnchor{
			LastRealPromptTokens:             rc.LastRealPromptTokens,
			LastRequestContextEstimateTokens: rc.LastRequestContextEstimateTokens,
		}
		if anchor.Valid() {
			return anchor, true
		}
	}
	if rc == nil {
		return ContextCompactPressureAnchor{}, false
	}
	anchor := latestContextCompactPressureAnchor(ctx, pool, rc.Run.AccountID, rc.Run.ThreadID)
	if anchor == nil || !anchor.Valid() {
		return ContextCompactPressureAnchor{}, false
	}
	return *anchor, true
}

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
	TargetChunkCount                 int
	PreviousReplacementCount         int
	SingleAtomPartial                bool
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
	if stats.TargetChunkCount > 0 {
		payload["target_chunk_count"] = stats.TargetChunkCount
	}
	if stats.PreviousReplacementCount > 0 {
		payload["previous_replacement_count"] = stats.PreviousReplacementCount
	}
	if stats.SingleAtomPartial {
		payload["single_atom_partial"] = true
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

// estimateContextCompactRequestBytes 按完整 request 口径估算 compact 当前请求大小。
func estimateContextCompactRequestBytes(rc *RunContext, systemPrompt string, msgs []llm.Message) int {
	request := buildContextCompactEstimateRequest(rc, systemPrompt, msgs)
	if rc != nil && rc.EstimateProviderRequestBytes != nil {
		estimated, err := rc.EstimateProviderRequestBytes(request)
		if err == nil && estimated > 0 {
			return estimated
		}
	}
	return llm.EstimateRequestJSONBytes(request)
}

func buildContextCompactEstimateRequest(rc *RunContext, systemPrompt string, msgs []llm.Message) llm.Request {
	request := llm.Request{
		Messages: contextCompactRequestMessages(systemPrompt, msgs),
	}
	if rc == nil {
		return request
	}
	if rc.SelectedRoute != nil {
		request.Model = strings.TrimSpace(rc.SelectedRoute.Route.Model)
	}
	if len(rc.FinalSpecs) > 0 {
		request.Tools = append([]llm.ToolSpec(nil), rc.FinalSpecs...)
	} else if len(rc.ToolSpecs) > 0 {
		request.Tools = append([]llm.ToolSpec(nil), rc.ToolSpecs...)
	}
	if rc.ToolChoice != nil {
		choice := *rc.ToolChoice
		request.ToolChoice = &choice
	}
	if rc.Temperature != nil {
		temperature := *rc.Temperature
		request.Temperature = &temperature
	}
	if rc.MaxOutputTokens != nil {
		maxOutput := *rc.MaxOutputTokens
		request.MaxOutputTokens = &maxOutput
	}
	request.ReasoningMode = strings.TrimSpace(rc.ReasoningMode)
	return request
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
	rows, err := pool.Query(ctx,
		latestContextCompactPressureAnchorSQL(),
		accountID,
		threadID,
	)
	if err != nil || rows == nil {
		return nil
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil || len(raw) == 0 {
			return nil
		}
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil
		}
		if scope, _ := payload["event_scope"].(string); scope == "context_compact" {
			continue
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
	return nil
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

const maxConsecutiveCompactFailures = 3

// compactConsecutiveFailures 查询 thread 最近的 compact 事件，返回从最新往前的连续失败次数。
func compactConsecutiveFailures(ctx context.Context, pool CompactPersistDB, accountID, threadID uuid.UUID) int {
	if pool == nil || accountID == uuid.Nil || threadID == uuid.Nil {
		return 0
	}
	rows, err := pool.Query(ctx,
		compactConsecutiveFailuresSQL(),
		accountID,
		threadID,
		maxConsecutiveCompactFailures*2+1,
	)
	if err != nil {
		return 0
	}
	if rows == nil {
		return 0
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return count
		}
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			return count
		}
		phase, _ := payload["phase"].(string)
		switch {
		case phase == "completed":
			return count
		case phase == "started" ||
			phase == "round_started" ||
			phase == "round_completed" ||
			phase == "evaluating" ||
			phase == "middleware_completed" ||
			phase == "llm_request_started" ||
			phase == "llm_request_completed" ||
			phase == "llm_request_retrying" ||
			phase == "circuit_breaker":
			// 中间态事件不计入失败，也不截断连续失败统计。
		case strings.Contains(phase, "failed") || payload["error"] != nil:
			count++
		default:
			return count
		}
	}
	return count
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
	if rc == nil || contextCompactHasSyntheticPrefix(rc.Messages) {
		return ContextCompactPressureAnchor{}, false
	}
	anchor := latestContextCompactPressureAnchor(ctx, pool, rc.Run.AccountID, rc.Run.ThreadID)
	if anchor == nil || !anchor.Valid() {
		return ContextCompactPressureAnchor{}, false
	}
	return *anchor, true
}

func contextCompactHasSyntheticPrefix(msgs []llm.Message) bool {
	for _, msg := range msgs {
		if msg.Phase != nil && strings.TrimSpace(*msg.Phase) == compactSyntheticPhase {
			return true
		}
		if strings.TrimSpace(msg.Role) == "system" {
			continue
		}
		break
	}
	return false
}

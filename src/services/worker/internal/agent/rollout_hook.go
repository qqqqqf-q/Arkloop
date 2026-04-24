package agent

import (
	"context"
	"encoding/json"
	"time"

	"arkloop/services/shared/rollout"
)

// MakeRunMeta 创建 RunMeta RolloutItem
func MakeRunMeta(runCtx RunContext) rollout.RolloutItem {
	payload := rollout.RunMeta{
		RunID:     runCtx.RunID.String(),
		AccountID: runCtx.AccountID.String(),
		CreatedAt: time.Now().Format(time.RFC3339),
	}
	data, _ := json.Marshal(payload)
	return rollout.RolloutItem{
		Type:      "run_meta",
		Timestamp: time.Now(),
		Payload:   data,
	}
}

// MakeTurnStart 创建 TurnStart RolloutItem
func MakeTurnStart(turnIndex int, model string) rollout.RolloutItem {
	payload := rollout.TurnStart{
		TurnIndex: turnIndex,
		Model:     model,
	}
	data, _ := json.Marshal(payload)
	return rollout.RolloutItem{
		Type:      "turn_start",
		Timestamp: time.Now(),
		Payload:   data,
	}
}

// MakeAssistantMessage 创建 AssistantMessage RolloutItem
func MakeAssistantMessage(content string, contentParts json.RawMessage, toolCalls json.RawMessage) rollout.RolloutItem {
	payload := rollout.AssistantMessage{
		Content:      content,
		ContentParts: contentParts,
		ToolCalls:    toolCalls,
	}
	data, _ := json.Marshal(payload)
	return rollout.RolloutItem{
		Type:      "assistant_message",
		Timestamp: time.Now(),
		Payload:   data,
	}
}

// MakeToolCall 创建 ToolCall RolloutItem
func MakeToolCall(callID, name string, input json.RawMessage) rollout.RolloutItem {
	payload := rollout.ToolCall{
		CallID: callID,
		Name:   name,
		Input:  input,
	}
	data, _ := json.Marshal(payload)
	return rollout.RolloutItem{
		Type:      "tool_call",
		Timestamp: time.Now(),
		Payload:   data,
	}
}

// MakeToolResult 创建 ToolResult RolloutItem
func MakeToolResult(callID string, output json.RawMessage, errMsg string) rollout.RolloutItem {
	payload := rollout.ToolResult{
		CallID: callID,
		Output: output,
		Error:  errMsg,
	}
	data, _ := json.Marshal(payload)
	return rollout.RolloutItem{
		Type:      "tool_result",
		Timestamp: time.Now(),
		Payload:   data,
	}
}

// MakeTurnEnd 创建 TurnEnd RolloutItem
func MakeTurnEnd(turnIndex int) rollout.RolloutItem {
	payload := rollout.TurnEnd{
		TurnIndex: turnIndex,
	}
	data, _ := json.Marshal(payload)
	return rollout.RolloutItem{
		Type:      "turn_end",
		Timestamp: time.Now(),
		Payload:   data,
	}
}

// MakeRunEnd 创建 RunEnd RolloutItem
func MakeRunEnd(status string) rollout.RolloutItem {
	payload := rollout.RunEnd{
		FinalStatus: status,
	}
	data, _ := json.Marshal(payload)
	return rollout.RolloutItem{
		Type:      "run_end",
		Timestamp: time.Now(),
		Payload:   data,
	}
}

// appendRollout 安全地追加 rollout 条目，recorder 为 nil 时不操作
func appendRollout(ctx context.Context, recorder *rollout.Recorder, item rollout.RolloutItem) {
	if recorder == nil {
		return
	}
	_ = recorder.Append(ctx, item)
}

// appendRolloutSync 安全地同步追加 rollout 条目，recorder 为 nil 时不操作
func appendRolloutSync(ctx context.Context, recorder *rollout.Recorder, item rollout.RolloutItem) {
	if recorder == nil {
		return
	}
	_ = recorder.AppendSync(ctx, item)
}

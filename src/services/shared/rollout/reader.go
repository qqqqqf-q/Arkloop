package rollout

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"arkloop/services/shared/objectstore"

	"github.com/google/uuid"
)

// Reader 从 S3 读取并解析 JSONL rollout 文件。
type Reader struct {
	store objectstore.BlobStore
}

func NewReader(store objectstore.BlobStore) *Reader {
	return &Reader{store: store}
}

type manifest struct {
	SchemaVersion int      `json:"schema_version"`
	Segments      []string `json:"segments"`
}

// ReadRollout 下载 S3 上的 JSONL 文件并解析为 RolloutItem 列表。
func (r *Reader) ReadRollout(ctx context.Context, runID uuid.UUID) ([]RolloutItem, error) {
	manifestKey := manifestKey(runID)
	if data, err := r.store.Get(ctx, manifestKey); err == nil {
		var m manifest
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("parse rollout manifest %s: %w", runID, err)
		}
		if len(m.Segments) == 0 {
			return r.readLegacy(ctx, runID)
		}
		var items []RolloutItem
		for _, segmentKey := range m.Segments {
			data, err := r.store.Get(ctx, segmentKey)
			if err != nil {
				return nil, err
			}
			parsed, err := parseRolloutItems(runID, data)
			if err != nil {
				return nil, err
			}
			items = append(items, parsed...)
		}
		return items, nil
	} else if err != nil && !objectstore.IsNotFound(err) && !looksLikeNotFound(err) {
		return nil, err
	}

	return r.readLegacy(ctx, runID)
}

func (r *Reader) readLegacy(ctx context.Context, runID uuid.UUID) ([]RolloutItem, error) {
	key := legacyRolloutKey(runID)
	data, err := r.store.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	return parseRolloutItems(runID, data)
}

func looksLikeNotFound(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "not found")
}

func parseRolloutItems(runID uuid.UUID, data []byte) ([]RolloutItem, error) {
	var items []RolloutItem
	reader := bufio.NewReader(bytes.NewReader(data))
	lineNum := 0
	for {
		chunk, readErr := reader.ReadBytes('\n')
		if len(chunk) == 0 && readErr == io.EOF {
			break
		}
		if readErr != nil && readErr != io.EOF {
			return nil, fmt.Errorf("read rollout %s: %w", runID, readErr)
		}
		lineNum++
		line := bytes.TrimSpace(chunk)
		if len(line) == 0 {
			if readErr == io.EOF {
				break
			}
			continue
		}
		var item RolloutItem
		if err := json.Unmarshal(line, &item); err != nil {
			return nil, fmt.Errorf("parse rollout line %d: %w", lineNum, err)
		}
		items = append(items, item)
		if readErr == io.EOF {
			break
		}
	}
	return items, nil
}

func HasRollout(ctx context.Context, store objectstore.BlobStore, runID uuid.UUID) (bool, error) {
	if _, err := store.Head(ctx, manifestKey(runID)); err == nil {
		return true, nil
	} else if err != nil && !objectstore.IsNotFound(err) {
		return false, err
	}
	if _, err := store.Head(ctx, legacyRolloutKey(runID)); err == nil {
		return true, nil
	} else if err != nil && !objectstore.IsNotFound(err) {
		return false, err
	}
	return false, nil
}

func manifestKey(runID uuid.UUID) string {
	return "run/" + runID.String() + "/manifest.json"
}

func segmentKey(runID uuid.UUID, index int) string {
	return fmt.Sprintf("run/%s/segments/%06d.jsonl", runID.String(), index)
}

func legacyRolloutKey(runID uuid.UUID) string {
	return "run/" + runID.String() + ".jsonl"
}

// Reconstruct 顺序扫描 RolloutItem 列表，重建 assistant/tool 回放序列和未完成 tool call。
func (r *Reader) Reconstruct(items []RolloutItem) *ReconstructedState {
	state := &ReconstructedState{}
	pending := map[string]ToolCall{}
	pendingOrder := make([]string, 0)
	toolNames := map[string]string{}
	currentTurnIndex := 0

	for _, item := range items {
		switch item.Type {
		case "run_end":
			var payload RunEnd
			if json.Unmarshal(item.Payload, &payload) == nil {
				state.FinalStatus = payload.FinalStatus
			}
		case "turn_start":
			var payload TurnStart
			if json.Unmarshal(item.Payload, &payload) == nil {
				currentTurnIndex = payload.TurnIndex
			}
		case "assistant_message":
			var payload AssistantMessage
			if json.Unmarshal(item.Payload, &payload) != nil {
				continue
			}
			state.Messages = append(state.Messages, item.Payload)
			state.ReplayMessages = append(state.ReplayMessages, ReplayMessage{
				Role:      "assistant",
				Assistant: &payload,
			})
		case "tool_call":
			var payload ToolCall
			if json.Unmarshal(item.Payload, &payload) != nil {
				continue
			}
			pending[payload.CallID] = payload
			toolNames[payload.CallID] = payload.Name
			pendingOrder = append(pendingOrder, payload.CallID)
		case "tool_result":
			var payload ToolResult
			if json.Unmarshal(item.Payload, &payload) != nil {
				continue
			}
			delete(pending, payload.CallID)
			state.ReplayMessages = append(state.ReplayMessages, ReplayMessage{
				Role: "tool",
				Tool: &ReplayToolResult{
					CallID: payload.CallID,
					Name:   toolNames[payload.CallID],
					Output: payload.Output,
					Error:  payload.Error,
				},
			})
		case "turn_end":
			var payload TurnEnd
			if json.Unmarshal(item.Payload, &payload) == nil {
				currentTurnIndex = payload.TurnIndex
			}
		}
	}

	for _, callID := range pendingOrder {
		call, ok := pending[callID]
		if !ok {
			continue
		}
		state.PendingToolCalls = append(state.PendingToolCalls, call)
	}
	if len(state.PendingToolCalls) > 0 {
		state.Breakpoint = &Breakpoint{TurnIndex: currentTurnIndex}
	}
	return state
}

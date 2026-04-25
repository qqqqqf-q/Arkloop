package llm

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"arkloop/services/worker/internal/stablejson"
)

func parseAnthropicAdvancedJSON(raw map[string]any) (anthropicAdvancedConfig, error) {
	cfg := anthropicAdvancedConfig{
		ExtraHeaders: map[string]string{},
		Payload:      map[string]any{},
	}
	if raw == nil {
		return cfg, nil
	}

	for key, value := range raw {
		switch key {
		case anthropicAdvancedKeyVersion:
			version, ok := value.(string)
			if !ok || strings.TrimSpace(version) == "" {
				return anthropicAdvancedConfig{}, anthropicAdvancedJSONError{
					Message: "advanced_json.anthropic_version must be a non-empty string",
				}
			}
			v := strings.TrimSpace(version)
			cfg.Version = &v
		case anthropicAdvancedKeyExtraHeaders:
			headers, ok := value.(map[string]any)
			if !ok {
				return anthropicAdvancedConfig{}, anthropicAdvancedJSONError{
					Message: "advanced_json.extra_headers must be an object",
				}
			}
			for hk, hv := range headers {
				headerName := strings.ToLower(strings.TrimSpace(hk))
				if headerName != anthropicBetaHeader {
					return anthropicAdvancedConfig{}, anthropicAdvancedJSONError{
						Message: "advanced_json.extra_headers only supports anthropic-beta",
						Details: map[string]any{"invalid_header": hk},
					}
				}
				headerValue, ok := hv.(string)
				if !ok || strings.TrimSpace(headerValue) == "" {
					return anthropicAdvancedConfig{}, anthropicAdvancedJSONError{
						Message: "advanced_json.extra_headers.anthropic-beta must be a non-empty string",
					}
				}
				cfg.ExtraHeaders[anthropicBetaHeader] = strings.TrimSpace(headerValue)
			}
		default:
			if _, denied := anthropicAdvancedJSONDenylist[key]; denied {
				return anthropicAdvancedConfig{}, anthropicAdvancedJSONError{
					Message: fmt.Sprintf("advanced_json must not set critical field: %s", key),
					Details: map[string]any{"denied_key": key},
				}
			}
			cfg.Payload[key] = value
		}
	}

	return cfg, nil
}

func toAnthropicMessages(messages []Message) ([]map[string]any, []map[string]any, error) {
	return toAnthropicMessagesWithPlan(messages, nil)
}

func toAnthropicMessagesWithPlan(messages []Message, plan *PromptPlan) ([]map[string]any, []map[string]any, error) {
	systemBlocks := []map[string]any{}
	out := []map[string]any{}
	pendingToolResults := []map[string]any{}
	lastAssistantToolUseIDs := map[string]struct{}{}
	// 记录最后一个带 tool_use 的 assistant 在 out 中的索引，便于回退
	lastToolUseAssistantIdx := -1
	sourceToOut := map[int]int{}
	userSourceToOut := map[int]int{}

	if len(messages) == 0 {
		return systemBlocks, out, nil
	}

	planSystemBlocks := anthropicSystemBlocksFromPlan(plan)
	if len(planSystemBlocks) > 0 {
		systemBlocks = append(systemBlocks, planSystemBlocks...)
	}

	flushToolResults := func() {
		if len(pendingToolResults) == 0 {
			// 没有 tool_result 但有 tool_use -> 回退 assistant 中的 tool_use blocks
			if lastToolUseAssistantIdx >= 0 && lastToolUseAssistantIdx < len(out) {
				stripToolUseBlocks(out, lastToolUseAssistantIdx, lastAssistantToolUseIDs)
			}
			lastAssistantToolUseIDs = map[string]struct{}{}
			lastToolUseAssistantIdx = -1
			return
		}
		filtered := make([]map[string]any, 0, len(pendingToolResults))
		matchedToolUseIDs := map[string]struct{}{}
		for _, block := range pendingToolResults {
			id, _ := block["tool_use_id"].(string)
			if _, ok := lastAssistantToolUseIDs[id]; ok {
				filtered = append(filtered, block)
				matchedToolUseIDs[id] = struct{}{}
			} else {
				prevRole := ""
				prevSummary := ""
				if len(out) > 0 {
					prev := out[len(out)-1]
					prevRole, _ = prev["role"].(string)
					if content, ok := prev["content"].([]map[string]any); ok && len(content) > 0 {
						if t, ok := content[0]["text"].(string); ok {
							if len(t) > 100 {
								t = t[:100]
							}
							prevSummary = t
						}
					} else if t, ok := prev["content"].(string); ok {
						if len(t) > 100 {
							t = t[:100]
						}
						prevSummary = t
					}
				}
				slog.Warn("dropped orphan tool_result", "tool_use_id", id, "prev_role", prevRole, "prev_content_summary", prevSummary)
			}
		}
		pendingToolResults = []map[string]any{}
		if len(filtered) == 0 {
			if lastToolUseAssistantIdx >= 0 && lastToolUseAssistantIdx < len(out) {
				stripToolUseBlocks(out, lastToolUseAssistantIdx, lastAssistantToolUseIDs)
			}
			lastAssistantToolUseIDs = map[string]struct{}{}
			lastToolUseAssistantIdx = -1
			return
		}
		if lastToolUseAssistantIdx >= 0 && len(matchedToolUseIDs) < len(lastAssistantToolUseIDs) {
			stripToolUseBlocks(out, lastToolUseAssistantIdx, subtractToolUseIDs(lastAssistantToolUseIDs, matchedToolUseIDs))
		}
		out = append(out, map[string]any{
			"role":    "user",
			"content": filtered,
		})
		lastAssistantToolUseIDs = map[string]struct{}{}
		lastToolUseAssistantIdx = -1
	}

	for sourceIndex, message := range messages {
		text := joinParts(message.Content)
		if message.Role == "system" {
			if len(planSystemBlocks) == 0 && strings.TrimSpace(text) != "" {
				block := map[string]any{"type": "text", "text": text}
				for _, part := range message.Content {
					if cc := anthropicCacheControlFromHints(part.CacheHint, part.CacheControl); cc != nil {
						block["cache_control"] = cc
						break
					}
				}
				systemBlocks = append(systemBlocks, block)
			}
			continue
		}

		if message.Role == "tool" {
			imageParts := collectImageParts(message.Content)
			block, err := anthropicToolResultBlock(text, imageParts)
			if err != nil {
				return nil, nil, err
			}
			pendingToolResults = append(pendingToolResults, block)
			continue
		}

		flushToolResults()

		if message.Role == "assistant" && len(message.ToolCalls) > 0 {
			lastAssistantToolUseIDs = make(map[string]struct{}, len(message.ToolCalls))
			blocks, err := anthropicContentBlocks(message.Content)
			if err != nil {
				return nil, nil, err
			}
			for _, call := range message.ToolCalls {
				call = CanonicalToolCall(call)
				lastAssistantToolUseIDs[call.ToolCallID] = struct{}{}
				blocks = append(blocks, map[string]any{
					"type":  "tool_use",
					"id":    call.ToolCallID,
					"name":  call.ToolName,
					"input": mapOrEmpty(call.ArgumentsJSON),
				})
			}
			lastToolUseAssistantIdx = len(out)
			out = append(out, map[string]any{
				"role":    "assistant",
				"content": blocks,
			})
			sourceToOut[sourceIndex] = len(out) - 1
			continue
		}

		lastAssistantToolUseIDs = map[string]struct{}{}
		lastToolUseAssistantIdx = -1

		blocks, err := anthropicContentBlocks(message.Content)
		if err != nil {
			return nil, nil, err
		}
		if len(blocks) == 0 {
			blocks = []map[string]any{{"type": "text", "text": text}}
		}
		out = append(out, map[string]any{
			"role":    message.Role,
			"content": blocks,
		})
		sourceToOut[sourceIndex] = len(out) - 1
		if message.Role == "user" {
			userSourceToOut[sourceIndex] = len(out) - 1
		}
	}

	flushToolResults()

	// strip 后可能出现内容为空的 assistant 消息，无条件移除，避免 API 报 "text is required"。
	compacted := make([]map[string]any, 0, len(out))
	for _, msg := range out {
		if msg["role"] == "assistant" && isEmptyAssistantMsg(msg) {
			continue
		}
		compacted = append(compacted, msg)
	}

	if plan != nil {
		applyAnthropicMessageCachePlan(compacted, sourceToOut, userSourceToOut, plan.MessageCache)
	}

	return systemBlocks, compacted, nil
}

func anthropicSystemBlocksFromPlan(plan *PromptPlan) []map[string]any {
	if plan == nil || len(plan.SystemBlocks) == 0 {
		return nil
	}
	type systemAccumulator struct {
		text       strings.Builder
		cacheType  string
		cacheScope string
	}

	flush := func(blocks []map[string]any, acc *systemAccumulator) []map[string]any {
		if acc == nil {
			return blocks
		}
		text := strings.TrimSpace(acc.text.String())
		if text == "" {
			return blocks
		}
		item := map[string]any{
			"type": "text",
			"text": text,
		}
		if acc.cacheType != "" {
			cc := map[string]any{"type": acc.cacheType}
			if acc.cacheScope != "" {
				cc["scope"] = acc.cacheScope
			}
			item["cache_control"] = cc
		}
		return append(blocks, item)
	}

	blocks := make([]map[string]any, 0, len(plan.SystemBlocks))
	var acc *systemAccumulator
	for _, block := range plan.SystemBlocks {
		text := strings.TrimSpace(block.Text)
		if text == "" {
			continue
		}
		cacheType := ""
		cacheScope := ""
		if block.CacheEligible {
			cacheHint := &CacheHint{
				Action: CacheHintActionWrite,
				Scope:  cacheScopeFromStability(block.Stability),
			}
			if cc := anthropicCacheControlFromHints(cacheHint, nil); cc != nil {
				cacheType, _ = cc["type"].(string)
				cacheScope, _ = cc["scope"].(string)
			}
		}
		if acc == nil || acc.cacheType != cacheType || acc.cacheScope != cacheScope {
			blocks = flush(blocks, acc)
			acc = &systemAccumulator{
				cacheType:  cacheType,
				cacheScope: cacheScope,
			}
		}
		if acc.text.Len() > 0 {
			acc.text.WriteString("\n\n")
		}
		acc.text.WriteString(text)
	}
	return flush(blocks, acc)
}

func cacheScopeFromStability(stability string) string {
	switch strings.ToLower(strings.TrimSpace(stability)) {
	case CacheStabilityStablePrefix:
		return "global"
	case CacheStabilitySessionPrefix:
		return "org"
	default:
		return ""
	}
}

func anthropicCacheControlFromHints(hint *CacheHint, legacyCacheControl *string) map[string]any {
	if hint != nil && strings.EqualFold(strings.TrimSpace(hint.Action), CacheHintActionWrite) {
		payload := map[string]any{"type": "ephemeral"}
		if scope := strings.TrimSpace(hint.Scope); scope != "" {
			payload["scope"] = scope
		}
		return payload
	}
	if legacyCacheControl != nil && strings.TrimSpace(*legacyCacheControl) != "" {
		return map[string]any{"type": strings.TrimSpace(*legacyCacheControl)}
	}
	return nil
}

func enforceAnthropicCacheControlLimit(payload map[string]any) {
	remaining := anthropicMaxCacheControlBlocks
	consume := func(block map[string]any) {
		if block == nil {
			return
		}
		if _, ok := block["cache_control"]; !ok {
			return
		}
		if remaining > 0 {
			remaining--
			return
		}
		delete(block, "cache_control")
	}

	if system, ok := payload["system"].([]map[string]any); ok {
		for _, block := range system {
			consume(block)
		}
	}
	if messages, ok := payload["messages"].([]map[string]any); ok {
		for _, message := range messages {
			content, ok := message["content"].([]map[string]any)
			if !ok {
				continue
			}
			for _, block := range content {
				consume(block)
			}
		}
	}
	if tools, ok := payload["tools"].([]map[string]any); ok {
		for _, tool := range tools {
			consume(tool)
		}
	}
}

func applyAnthropicMessageCachePlan(out []map[string]any, sourceToOut map[int]int, userSourceToOut map[int]int, plan MessageCachePlan) {
	if len(out) == 0 {
		return
	}

	if plan.Enabled {
		clearMessageCacheControl(out)
		markerOutIdx := resolveMarkerMessageIndex(plan.MarkerMessageIndex, sourceToOut, len(out))
		if plan.SkipCacheWrite && markerOutIdx > 0 {
			markerOutIdx--
		}
		applySingleMessageCacheMarker(out, markerOutIdx)
		if plan.StableMarkerEnabled {
			stableOutIdx := resolveStableMarkerMessageIndex(plan.StableMarkerMessageIndex, sourceToOut)
			if stableOutIdx >= 0 && stableOutIdx < markerOutIdx {
				applySingleMessageCacheMarker(out, stableOutIdx)
			}
		}
	}

	applyCacheEdits(out, userSourceToOut, plan)
}

func clearMessageCacheControl(messages []map[string]any) {
	for _, msg := range messages {
		content, ok := msg["content"].([]map[string]any)
		if !ok {
			continue
		}
		for _, block := range content {
			delete(block, "cache_control")
		}
	}
}

func resolveMarkerMessageIndex(sourceIndex int, sourceToOut map[int]int, outLen int) int {
	if outLen == 0 {
		return -1
	}
	if sourceIndex >= 0 {
		if idx, ok := sourceToOut[sourceIndex]; ok {
			return idx
		}
	}
	return outLen - 1
}

func resolveStableMarkerMessageIndex(sourceIndex int, sourceToOut map[int]int) int {
	if sourceIndex < 0 {
		return -1
	}
	if idx, ok := sourceToOut[sourceIndex]; ok {
		return idx
	}
	return -1
}

func resolveToolResultCacheCutIndex(sourceIndex int, sourceToOut map[int]int, fallback int) int {
	if sourceIndex >= 0 {
		if idx, ok := sourceToOut[sourceIndex]; ok {
			return idx
		}
	}
	return fallback
}

func applySingleMessageCacheMarker(messages []map[string]any, markerOutIdx int) {
	for i := markerOutIdx; i >= 0 && i < len(messages); i-- {
		msg := messages[i]
		content, ok := msg["content"].([]map[string]any)
		if !ok || len(content) == 0 {
			continue
		}
		for j := len(content) - 1; j >= 0; j-- {
			block := content[j]
			blockType, _ := block["type"].(string)
			if !anthropicCacheMarkerBlockType(blockType) {
				continue
			}
			block["cache_control"] = map[string]any{"type": "ephemeral"}
			return
		}
	}
}

func anthropicCacheMarkerBlockType(blockType string) bool {
	switch strings.TrimSpace(blockType) {
	case "text", "tool_result", "tool_use":
		return true
	default:
		return false
	}
}

func applyCacheEdits(messages []map[string]any, userSourceToOut map[int]int, plan MessageCachePlan) {
	seenReferences := map[string]struct{}{}
	for _, block := range plan.PinnedCacheEdits {
		applyCacheEditsBlockAt(messages, userSourceToOut, block, seenReferences)
	}
	if plan.NewCacheEdits != nil {
		applyCacheEditsBlockAt(messages, userSourceToOut, *plan.NewCacheEdits, seenReferences)
	}
}

func applyCacheEditsBlockAt(messages []map[string]any, userSourceToOut map[int]int, block PromptCacheEditsBlock, seen map[string]struct{}) {
	if len(messages) == 0 {
		return
	}
	outIndex := -1
	if block.UserMessageIndex >= 0 {
		if idx, ok := userSourceToOut[block.UserMessageIndex]; ok {
			outIndex = idx
		}
	}
	if outIndex < 0 {
		for i := len(messages) - 1; i >= 0; i-- {
			role, _ := messages[i]["role"].(string)
			if role == "user" {
				outIndex = i
				break
			}
		}
	}
	if outIndex < 0 || outIndex >= len(messages) {
		return
	}
	cacheEditsBlock := buildAnthropicCacheEditsBlock(block, seen)
	if cacheEditsBlock == nil {
		return
	}
	content, ok := messages[outIndex]["content"].([]map[string]any)
	if !ok || len(content) == 0 {
		messages[outIndex]["content"] = []map[string]any{cacheEditsBlock}
		return
	}
	content = append(content, cacheEditsBlock)
	messages[outIndex]["content"] = content
}

func buildAnthropicCacheEditsBlock(block PromptCacheEditsBlock, seen map[string]struct{}) map[string]any {
	edits := make([]map[string]any, 0, len(block.Edits))
	for _, edit := range block.Edits {
		action := strings.ToLower(strings.TrimSpace(edit.Type))
		if action == "" {
			action = CacheHintActionDelete
		}
		ref := strings.TrimSpace(edit.CacheReference)
		if ref == "" {
			continue
		}
		if _, exists := seen[ref]; exists {
			continue
		}
		seen[ref] = struct{}{}
		edits = append(edits, map[string]any{
			"type":            action,
			"cache_reference": ref,
		})
	}
	if len(edits) == 0 {
		return nil
	}
	return map[string]any{
		"type":  "cache_edits",
		"edits": edits,
	}
}

func subtractToolUseIDs(all map[string]struct{}, keep map[string]struct{}) map[string]struct{} {
	if len(all) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(all))
	for id := range all {
		if _, ok := keep[id]; ok {
			continue
		}
		out[id] = struct{}{}
	}
	return out
}

// stripToolUseBlocks 从 out[idx] 的 content 中移除所有 tool_use blocks。
// 如果移除后 content 为空，整条消息也从 out 中清除（置为空 assistant）。
func stripToolUseBlocks(out []map[string]any, idx int, toolUseIDs map[string]struct{}) {
	msg := out[idx]
	content, ok := msg["content"].([]map[string]any)
	if !ok {
		return
	}
	filtered := make([]map[string]any, 0, len(content))
	for _, block := range content {
		if block["type"] == "tool_use" {
			if id, _ := block["id"].(string); id != "" {
				if _, match := toolUseIDs[id]; match {
					slog.Warn("stripped orphan tool_use from assistant", "tool_use_id", id)
					continue
				}
			}
		}
		filtered = append(filtered, block)
	}
	if len(filtered) == 0 {
		out[idx]["content"] = []map[string]any{}
		return
	}
	out[idx]["content"] = filtered
}

// isEmptyAssistantMsg 判断一条 assistant 消息是否仅含空 text block（strip 后的残留）。
func isEmptyAssistantMsg(msg map[string]any) bool {
	blocks, ok := msg["content"].([]map[string]any)
	if !ok {
		return false
	}
	for _, b := range blocks {
		if b["type"] != "text" {
			return false
		}
		if t, _ := b["text"].(string); strings.TrimSpace(t) != "" {
			return false
		}
	}
	return true
}

func anthropicContentBlocks(parts []ContentPart) ([]map[string]any, error) {
	blocks := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		switch part.Kind() {
		case "text":
			if strings.TrimSpace(part.Text) == "" {
				continue
			}
			block := map[string]any{"type": "text", "text": part.Text}
			if cc := anthropicCacheControlFromHints(part.CacheHint, part.CacheControl); cc != nil {
				block["cache_control"] = cc
			}
			blocks = append(blocks, block)
		case "thinking":
			signature := strings.TrimSpace(part.Signature)
			block := map[string]any{
				"type":     "thinking",
				"thinking": part.Text,
			}
			if signature != "" {
				block["signature"] = signature
			}
			blocks = append(blocks, block)
		case "redacted_thinking":
			blocks = append(blocks, map[string]any{
				"type": "redacted_thinking",
				"data": part.Text,
			})
		case "file":
			text := PartPromptText(part)
			if strings.TrimSpace(text) == "" {
				continue
			}
			blocks = append(blocks, map[string]any{"type": "text", "text": text})
		case "image":
			mimeType, data, err := modelInputImage(part)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(part.Attachment.Key) != "" {
				blocks = append(blocks, map[string]any{
					"type": "text",
					"text": "[attachment_key:" + part.Attachment.Key + "]",
				})
			}
			blocks = append(blocks, map[string]any{
				"type": "image",
				"source": map[string]any{
					"type":       "base64",
					"media_type": mimeType,
					"data":       base64.StdEncoding.EncodeToString(data),
				},
			})
		}
	}
	return blocks, nil
}

func anthropicToolResultBlock(text string, imageParts []ContentPart) (map[string]any, error) {
	var parsed any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return nil, fmt.Errorf("tool message is not valid JSON")
	}
	envelope, ok := parsed.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("tool message is not valid JSON")
	}

	toolCallID, _ := envelope["tool_call_id"].(string)
	toolCallID = strings.TrimSpace(toolCallID)
	if toolCallID == "" {
		return nil, fmt.Errorf("tool message missing tool_call_id")
	}

	isError := false
	var contentSource any
	if errObj, ok := envelope["error"]; ok && errObj != nil {
		isError = true
		contentSource = map[string]any{"error": errObj}
	} else {
		contentSource = envelope["result"]
	}

	contentText, err := stablejson.Encode(contentSource)
	if err != nil {
		contentText = "{}"
	}

	block := map[string]any{
		"type":        "tool_result",
		"tool_use_id": toolCallID,
	}
	if isError {
		block["is_error"] = true
	}

	if len(imageParts) == 0 {
		block["content"] = contentText
		return block, nil
	}

	// content block 数组：text + image blocks
	contentBlocks := []map[string]any{
		{"type": "text", "text": contentText},
	}
	for _, part := range imageParts {
		mimeType, data, err := modelInputImage(part)
		if err != nil {
			return nil, err
		}
		contentBlocks = append(contentBlocks, map[string]any{
			"type": "image",
			"source": map[string]any{
				"type":       "base64",
				"media_type": mimeType,
				"data":       base64.StdEncoding.EncodeToString(data),
			},
		})
	}
	block["content"] = contentBlocks
	return block, nil
}

func collectImageParts(parts []ContentPart) []ContentPart {
	var images []ContentPart
	for _, part := range parts {
		if part.Kind() == "image" && part.Attachment != nil && len(part.Data) > 0 {
			images = append(images, part)
		}
	}
	return images
}

func anthropicToolChoice(tc *ToolChoice) map[string]any {
	if tc == nil {
		return nil
	}
	switch tc.Mode {
	case "required":
		return map[string]any{"type": "any"}
	case "specific":
		return map[string]any{"type": "tool", "name": CanonicalToolName(tc.ToolName)}
	default:
		return nil
	}
}

func toAnthropicTools(specs []ToolSpec) []map[string]any {
	sortedSpecs := append([]ToolSpec(nil), specs...)
	sort.SliceStable(sortedSpecs, func(i, j int) bool {
		left := CanonicalToolName(sortedSpecs[i].Name)
		if left == "" {
			left = sortedSpecs[i].Name
		}
		right := CanonicalToolName(sortedSpecs[j].Name)
		if right == "" {
			right = sortedSpecs[j].Name
		}
		return left < right
	})

	out := make([]map[string]any, 0, len(sortedSpecs))
	for _, spec := range sortedSpecs {
		name := CanonicalToolName(spec.Name)
		if name == "" {
			name = spec.Name
		}
		payload := map[string]any{
			"name":         name,
			"input_schema": mapOrEmpty(spec.JSONSchema),
		}
		if spec.Description != nil {
			payload["description"] = *spec.Description
		}
		if cc := anthropicCacheControlFromHints(spec.CacheHint, nil); cc != nil {
			payload["cache_control"] = cc
		}
		out = append(out, payload)
	}
	return out
}

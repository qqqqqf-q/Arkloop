package subagentctl

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"arkloop/services/shared/skillstore"
	"arkloop/services/worker/internal/data"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type SnapshotStorage struct {
	repo data.SubAgentContextSnapshotsRepository
}

func NewSnapshotStorage() *SnapshotStorage {
	return &SnapshotStorage{}
}

func (s *SnapshotStorage) Save(ctx context.Context, tx pgx.Tx, subAgentID uuid.UUID, snapshot ContextSnapshot) error {
	if s == nil {
		return fmt.Errorf("snapshot storage must not be nil")
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("marshal context snapshot: %w", err)
	}
	return s.repo.Upsert(ctx, tx, subAgentID, payload)
}

func (s *SnapshotStorage) LoadBySubAgent(ctx context.Context, tx pgx.Tx, subAgentID uuid.UUID) (*ContextSnapshot, error) {
	if s == nil {
		return nil, fmt.Errorf("snapshot storage must not be nil")
	}
	record, err := s.repo.GetBySubAgentID(ctx, tx, subAgentID)
	if err != nil || record == nil {
		return nil, err
	}
	return decodeContextSnapshot(record.SnapshotJSON)
}

func (s *SnapshotStorage) LoadByCurrentRun(ctx context.Context, db data.DB, runID uuid.UUID) (*ContextSnapshot, error) {
	if s == nil {
		return nil, fmt.Errorf("snapshot storage must not be nil")
	}
	if db == nil {
		return nil, fmt.Errorf("pool must not be nil")
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := s.LoadByCurrentRunTx(ctx, tx, runID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *SnapshotStorage) LoadByCurrentRunTx(ctx context.Context, tx pgx.Tx, runID uuid.UUID) (*ContextSnapshot, error) {
	if s == nil {
		return nil, fmt.Errorf("snapshot storage must not be nil")
	}
	record, err := s.repo.GetByCurrentRunID(ctx, tx, runID)
	if err != nil || record == nil {
		return nil, err
	}
	return decodeContextSnapshot(record.SnapshotJSON)
}

func decodeContextSnapshot(raw []byte) (*ContextSnapshot, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var snapshot ContextSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, fmt.Errorf("decode context snapshot: %w", err)
	}
	return &snapshot, nil
}

type SnapshotBuilder struct {
	messages data.MessagesRepository
}

func NewSnapshotBuilder() *SnapshotBuilder {
	return &SnapshotBuilder{}
}

func (b *SnapshotBuilder) Build(ctx context.Context, tx pgx.Tx, parentRun data.Run, req ResolvedSpawnRequest) (ContextSnapshot, error) {
	if tx == nil {
		return ContextSnapshot{}, fmt.Errorf("tx must not be nil")
	}
	snapshot := ContextSnapshot{
		ContextMode: req.ContextMode,
		Inherit:     req.Inherit,
		Environment: ContextSnapshotEnvironment{},
		Routing:     snapshotRouting(req.ParentContext),
		Runtime:     ContextSnapshotRuntime{},
		Memory:      ContextSnapshotMemory{Scope: req.Inherit.MemoryScope},
	}

	if req.Inherit.Messages {
		messages, err := b.loadMessages(ctx, tx, parentRun, req)
		if err != nil {
			return ContextSnapshot{}, err
		}
		snapshot.Messages = messages
	}
	if req.Inherit.Workspace {
		snapshot.Environment.ProfileRef = strings.TrimSpace(derefString(parentRun.ProfileRef))
		snapshot.Environment.WorkspaceRef = strings.TrimSpace(derefString(parentRun.WorkspaceRef))
	}
	if req.Inherit.Skills {
		snapshot.Skills = cloneResolvedSkills(req.ParentContext.EnabledSkills)
	}
	if req.Inherit.Runtime {
		snapshot.Runtime = ContextSnapshotRuntime{
			ToolAllowlist: sortedUniqueStrings(req.ParentContext.ToolAllowlist),
			ToolDenylist:  sortedUniqueStrings(req.ParentContext.ToolDenylist),
			RouteID:       strings.TrimSpace(req.ParentContext.RouteID),
			Model:         strings.TrimSpace(req.ParentContext.Model),
		}
	}
	if req.Inherit.Messages && req.ParentContext.PromptCache != nil {
		snapshot.PromptCache = ClonePromptCacheSnapshot(req.ParentContext.PromptCache)
	}
	return snapshot, nil
}

func (b *SnapshotBuilder) loadMessages(ctx context.Context, tx pgx.Tx, parentRun data.Run, req ResolvedSpawnRequest) ([]ContextSnapshotMessage, error) {
	var (
		items []data.ThreadMessage
		err   error
	)
	switch req.ContextMode {
	case data.SubAgentContextModeForkRecent:
		items, err = b.messages.ListRecentByThread(ctx, tx, parentRun.AccountID, parentRun.ThreadID, forkRecentMessageLimit)
	case data.SubAgentContextModeForkThread:
		items, err = b.messages.ListByThread(ctx, tx, parentRun.AccountID, parentRun.ThreadID, 1000000)
	case data.SubAgentContextModeForkSelected:
		ids := parseUUIDStrings(req.Inherit.MessageIDs)
		items, err = b.messages.ListByIDs(ctx, tx, parentRun.AccountID, parentRun.ThreadID, ids)
		if err == nil && len(items) != len(ids) {
			return nil, fmt.Errorf("fork_selected messages must all belong to parent thread")
		}
	default:
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]ContextSnapshotMessage, 0, len(items))
	for _, item := range items {
		result = append(result, ContextSnapshotMessage{
			SourceMessageID: item.ID.String(),
			Role:            item.Role,
			Content:         item.Content,
			ContentJSON:     cloneRawJSON(item.ContentJSON),
			CreatedAt:       item.CreatedAt,
		})
	}
	result = trimLeadingOrphanToolMessages(result)
	return repairUnclosedToolCalls(result), nil
}

func snapshotRouting(parent SpawnParentContext) *ContextSnapshotRouting {
	routeID := strings.TrimSpace(parent.RouteID)
	model := strings.TrimSpace(parent.Model)
	if routeID == "" && model == "" {
		return nil
	}
	return &ContextSnapshotRouting{
		RouteID: routeID,
		Model:   model,
	}
}

func cloneResolvedSkills(items []skillstore.ResolvedSkill) []skillstore.ResolvedSkill {
	if len(items) == 0 {
		return nil
	}
	out := make([]skillstore.ResolvedSkill, len(items))
	copy(out, items)
	return out
}

func sortedUniqueStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		cleaned := strings.TrimSpace(item)
		if cleaned == "" {
			continue
		}
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		out = append(out, cleaned)
	}
	sort.Strings(out)
	return out
}

func parseUUIDStrings(items []string) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(items))
	for _, item := range items {
		parsed, err := uuid.Parse(strings.TrimSpace(item))
		if err != nil {
			continue
		}
		out = append(out, parsed)
	}
	return out
}

func cloneRawJSON(raw []byte) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	cloned := make([]byte, len(raw))
	copy(cloned, raw)
	return json.RawMessage(cloned)
}

// trimLeadingOrphanToolMessages removes leading tool messages that have no
// matching assistant tool_call before them (caused by LIMIT truncation).
func trimLeadingOrphanToolMessages(msgs []ContextSnapshotMessage) []ContextSnapshotMessage {
	start := 0
	for start < len(msgs) && msgs[start].Role == "tool" {
		start++
	}
	if start == 0 {
		return msgs
	}
	return msgs[start:]
}

// repairUnclosedToolCalls 确保 fork 出的消息历史中不存在未闭合的工具调用。
// 扫描 content_json 中的 tool_use/tool_result 块，对缺少 tool_result 的
// tool_use 补充合成的闭包消息。
func repairUnclosedToolCalls(messages []ContextSnapshotMessage) []ContextSnapshotMessage {
	if len(messages) == 0 {
		return messages
	}

	type toolUseEntry struct {
		id       string
		name     string
		afterIdx int
	}

	var openCalls []toolUseEntry
	closedIDs := map[string]struct{}{}

	for i, msg := range messages {
		if len(msg.ContentJSON) == 0 {
			continue
		}

		if msg.Role == "tool" {
			var toolMsg map[string]any
			if json.Unmarshal(msg.ContentJSON, &toolMsg) == nil {
				if toolUseID, _ := toolMsg["tool_use_id"].(string); toolUseID != "" {
					closedIDs[toolUseID] = struct{}{}
				}
			}
		}

		var blocks []json.RawMessage
		if json.Unmarshal(msg.ContentJSON, &blocks) != nil {
			continue
		}
		for _, raw := range blocks {
			var block map[string]any
			if json.Unmarshal(raw, &block) != nil {
				continue
			}
			blockType, _ := block["type"].(string)
			switch blockType {
			case "tool_use":
				toolID, _ := block["id"].(string)
				toolName, _ := block["name"].(string)
				if toolID != "" {
					openCalls = append(openCalls, toolUseEntry{id: toolID, name: toolName, afterIdx: i})
				}
			case "tool_result":
				if toolUseID, _ := block["tool_use_id"].(string); toolUseID != "" {
					closedIDs[toolUseID] = struct{}{}
				}
			}
		}
	}

	unclosed := make([]toolUseEntry, 0, len(openCalls))
	for _, entry := range openCalls {
		if _, ok := closedIDs[entry.id]; !ok {
			unclosed = append(unclosed, entry)
		}
	}
	if len(unclosed) == 0 {
		return messages
	}

	closuresByIdx := map[int][]ContextSnapshotMessage{}
	for _, entry := range unclosed {
		closureContent := fmt.Sprintf("[%s: context forked before completion]", entry.name)
		closureBlock, _ := json.Marshal([]map[string]any{{
			"type":        "tool_result",
			"tool_use_id": entry.id,
			"content":     closureContent,
			"is_error":    false,
		}})
		closuresByIdx[entry.afterIdx] = append(closuresByIdx[entry.afterIdx], ContextSnapshotMessage{
			Role:        "tool",
			Content:     closureContent,
			ContentJSON: closureBlock,
			CreatedAt:   messages[entry.afterIdx].CreatedAt,
		})
	}

	result := make([]ContextSnapshotMessage, 0, len(messages)+len(unclosed))
	for i, msg := range messages {
		result = append(result, msg)
		if closures, ok := closuresByIdx[i]; ok {
			result = append(result, closures...)
		}
	}
	return result
}

func isSpawnToolName(name string) bool {
	switch name {
	case "spawn_agent", "send_input", "wait_agent", "resume_agent", "close_agent", "interrupt_agent":
		return true
	}
	return false
}

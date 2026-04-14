package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"arkloop/services/shared/telegrambot"
)

const (
	progressEditMinInterval = time.Second
	progressMaxSegments     = 6
	progressMaxItemsPerSeg  = 4
	progressMaxSummaryItems = 3
	progressLiveProgress    = "In process"
	progressCompletedTitle  = "Completed"
	progressFallbackSummary = "In progress"
)

type telegramProgressRunSegment struct {
	ID    string
	Kind  string
	Mode  string
	Label string
}

type ProgressEntry struct {
	ToolCallID  string
	ToolName    string
	DisplayName string
	Brief       string
	Done        bool
	ErrorClass  string
}

type TelegramProgressSegment struct {
	ID         string
	Title      string
	RunSegment telegramProgressRunSegment
	Entries    []ProgressEntry
	Closed     bool
	MessageID  int64
	LastText   string
}

// TelegramProgressTracker keeps Telegram progress in the same segment-based order
// as the web COP view, while each Telegram segment owns its own message.
type TelegramProgressTracker struct {
	client  *telegrambot.Client
	token   string
	target  ChannelDeliveryTarget
	replyTo *ChannelMessageRef

	mu              sync.Mutex
	segments        []TelegramProgressSegment
	current         *TelegramProgressSegment
	activeRun       telegramProgressRunSegment
	nextSyntheticID int
	lastEdit        time.Time
	dirty           bool
}

func NewTelegramProgressTracker(
	client *telegrambot.Client,
	token string,
	target ChannelDeliveryTarget,
	replyTo *ChannelMessageRef,
) *TelegramProgressTracker {
	return &TelegramProgressTracker{
		client:  client,
		token:   token,
		target:  target,
		replyTo: replyTo,
	}
}

func (t *TelegramProgressTracker) MessageID() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.current != nil {
		return t.current.MessageID
	}
	if len(t.segments) == 0 {
		return 0
	}
	return t.segments[len(t.segments)-1].MessageID
}

func (t *TelegramProgressTracker) OnRunSegmentStart(ctx context.Context, segmentID, kind, mode, label string) {
	t.mu.Lock()
	var closed *TelegramProgressSegment
	if t.activeRun.ID != "" && t.activeRun.ID != segmentID {
		closed = t.closeCurrentLocked()
	}
	t.activeRun = telegramProgressRunSegment{
		ID:    strings.TrimSpace(segmentID),
		Kind:  strings.TrimSpace(kind),
		Mode:  strings.TrimSpace(mode),
		Label: strings.TrimSpace(label),
	}
	changed := false
	if t.current != nil && t.current.RunSegment.ID == "" {
		t.current.RunSegment = t.activeRun
		changed = true
	}
	if changed {
		t.dirty = true
	}
	t.mu.Unlock()
	if closed != nil {
		t.syncSegment(ctx, *closed, true)
	}
	if changed {
		t.tryEditCurrent(ctx, false)
	}
}

func (t *TelegramProgressTracker) OnRunSegmentEnd(ctx context.Context, segmentID string) {
	segmentID = strings.TrimSpace(segmentID)
	t.mu.Lock()
	var closed *TelegramProgressSegment
	if segmentID != "" && t.current != nil && t.current.RunSegment.ID == segmentID {
		closed = t.closeCurrentLocked()
	}
	if segmentID != "" && t.activeRun.ID == segmentID {
		t.activeRun = telegramProgressRunSegment{}
	}
	t.mu.Unlock()
	if closed != nil {
		t.syncSegment(ctx, *closed, true)
	}
}

func (t *TelegramProgressTracker) OnMessageDelta(ctx context.Context, role, channel, delta string) {
	if strings.TrimSpace(delta) == "" {
		return
	}
	if role = strings.TrimSpace(role); role != "" && role != "assistant" {
		return
	}
	if strings.TrimSpace(channel) != "" {
		return
	}
	t.mu.Lock()
	closed := t.closeCurrentLocked()
	t.mu.Unlock()
	if closed != nil {
		t.syncSegment(ctx, *closed, true)
	}
}

func (t *TelegramProgressTracker) OnToolCall(ctx context.Context, toolCallID, toolName, argsJSON string) {
	if isHiddenTelegramProgressTool(toolName) {
		return
	}

	canonical := canonicalToolName(toolName)
	displayName := displayToolName(toolName)
	brief := toolBrief(canonical, argsJSON)

	t.mu.Lock()
	seg := t.ensureCurrentLocked()
	seg.Entries = append(seg.Entries, ProgressEntry{
		ToolCallID:  strings.TrimSpace(toolCallID),
		ToolName:    canonical,
		DisplayName: displayName,
		Brief:       brief,
	})
	t.dirty = true
	t.mu.Unlock()
	t.tryEditCurrent(ctx, false)
}

func (t *TelegramProgressTracker) OnToolResult(ctx context.Context, toolCallID, toolName, errorClass string) {
	if isHiddenTelegramProgressTool(toolName) {
		return
	}

	canonical := canonicalToolName(toolName)
	displayName := displayToolName(toolName)
	errClass := strings.TrimSpace(errorClass)

	t.mu.Lock()
	if t.current != nil && markResultInEntries(t.current.Entries, strings.TrimSpace(toolCallID), canonical, displayName, errClass) {
		t.dirty = true
		t.mu.Unlock()
		t.tryEditCurrent(ctx, false)
		return
	}
	for i := len(t.segments) - 1; i >= 0; i-- {
		if !markResultInEntries(t.segments[i].Entries, strings.TrimSpace(toolCallID), canonical, displayName, errClass) {
			continue
		}
		snapshot := cloneTelegramProgressSegment(t.segments[i])
		t.mu.Unlock()
		t.syncSegment(ctx, snapshot, true)
		return
	}
	seg := t.ensureCurrentLocked()
	seg.Entries = append(seg.Entries, ProgressEntry{
		ToolCallID:  strings.TrimSpace(toolCallID),
		ToolName:    canonical,
		DisplayName: displayName,
		Done:        true,
		ErrorClass:  errClass,
	})
	t.dirty = true
	t.mu.Unlock()
	t.tryEditCurrent(ctx, false)
}

func (t *TelegramProgressTracker) Finalize(ctx context.Context) {
	t.mu.Lock()
	closed := t.closeCurrentLocked()
	t.mu.Unlock()
	if closed != nil {
		t.syncSegment(ctx, *closed, true)
	}
}

func (t *TelegramProgressTracker) ensureCurrentLocked() *TelegramProgressSegment {
	if t.current != nil {
		return t.current
	}
	t.nextSyntheticID++
	t.current = &TelegramProgressSegment{
		ID:         fmt.Sprintf("tg-progress-%d", t.nextSyntheticID),
		RunSegment: t.activeRun,
	}
	t.lastEdit = time.Time{}
	return t.current
}

func (t *TelegramProgressTracker) closeCurrentLocked() *TelegramProgressSegment {
	if t.current == nil {
		return nil
	}
	if len(t.current.Entries) == 0 {
		t.current = nil
		t.lastEdit = time.Time{}
		t.dirty = false
		return nil
	}
	closed := cloneTelegramProgressSegment(*t.current)
	closed.Closed = true
	t.segments = append(t.segments, closed)
	t.current = nil
	t.lastEdit = time.Time{}
	t.dirty = false
	return &closed
}

func markResultInEntries(entries []ProgressEntry, toolCallID, toolName, displayName, errorClass string) bool {
	for i := range entries {
		if entries[i].ToolCallID == toolCallID && toolCallID != "" {
			entries[i].Done = true
			if strings.TrimSpace(entries[i].DisplayName) == "" {
				entries[i].DisplayName = displayName
			}
			if strings.TrimSpace(entries[i].ToolName) == "" {
				entries[i].ToolName = toolName
			}
			entries[i].ErrorClass = errorClass
			return true
		}
	}
	return false
}

func cloneTelegramProgressSegment(seg TelegramProgressSegment) TelegramProgressSegment {
	out := seg
	out.Entries = append([]ProgressEntry(nil), seg.Entries...)
	return out
}

func (t *TelegramProgressTracker) updateSegmentDelivery(segmentID string, messageID int64, text string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.current != nil && t.current.ID == segmentID {
		if messageID != 0 {
			t.current.MessageID = messageID
		}
		t.current.LastText = text
		return
	}
	for i := range t.segments {
		if t.segments[i].ID != segmentID {
			continue
		}
		if messageID != 0 {
			t.segments[i].MessageID = messageID
		}
		t.segments[i].LastText = text
		return
	}
}

func (t *TelegramProgressTracker) tryEditCurrent(ctx context.Context, force bool) {
	t.mu.Lock()
	if t.current == nil || len(t.current.Entries) == 0 {
		t.mu.Unlock()
		return
	}
	if !t.dirty && !force {
		t.mu.Unlock()
		return
	}
	if !force && time.Since(t.lastEdit) < progressEditMinInterval {
		t.mu.Unlock()
		return
	}

	snapshot := cloneTelegramProgressSegment(*t.current)
	t.lastEdit = time.Now()
	t.dirty = false
	t.mu.Unlock()
	t.syncSegment(ctx, snapshot, false)
}

func (t *TelegramProgressTracker) syncSegment(ctx context.Context, seg TelegramProgressSegment, completed bool) {
	text := formatProgressSegment(seg, completed)
	if strings.TrimSpace(text) == "" {
		return
	}
	if seg.MessageID == 0 {
		messageID := t.sendInitial(ctx, text)
		if messageID != 0 {
			t.updateSegmentDelivery(seg.ID, messageID, text)
		}
		return
	}
	if seg.LastText == text {
		return
	}
	if t.editExisting(ctx, seg.MessageID, text) {
		t.updateSegmentDelivery(seg.ID, seg.MessageID, text)
	}
}

func (t *TelegramProgressTracker) sendInitial(ctx context.Context, text string) int64 {
	req := telegrambot.SendMessageRequest{
		ChatID: t.target.Conversation.Target,
		Text:   text,
	}
	if t.replyTo != nil && strings.TrimSpace(t.replyTo.MessageID) != "" {
		req.ReplyToMessageID = t.replyTo.MessageID
	}
	if t.target.Conversation.ThreadID != nil {
		req.MessageThreadID = *t.target.Conversation.ThreadID
	}
	sent, err := t.client.SendMessage(ctx, t.token, req)
	if err != nil {
		slog.WarnContext(ctx, "telegram progress: send failed", "err", err.Error())
		return 0
	}
	if sent == nil || sent.MessageID == 0 {
		return 0
	}
	return sent.MessageID
}

func (t *TelegramProgressTracker) editExisting(ctx context.Context, messageID int64, text string) bool {
	req := telegrambot.EditMessageTextRequest{
		ChatID:    t.target.Conversation.Target,
		MessageID: messageID,
		Text:      text,
	}
	if t.target.Conversation.ThreadID != nil {
		req.MessageThreadID = *t.target.Conversation.ThreadID
	}
	if err := t.client.EditMessageText(ctx, t.token, req); err != nil {
		slog.WarnContext(ctx, "telegram progress: edit failed", "err", err.Error())
		return false
	}
	return true
}

func (t *TelegramProgressTracker) formatProgressLocked(finalize bool) string {
	segments := t.visibleSegmentsLocked(finalize)
	if len(segments) == 0 {
		return ""
	}

	var b strings.Builder
	hidden := 0
	if len(segments) > progressMaxSegments {
		hidden = len(segments) - progressMaxSegments
		segments = segments[hidden:]
	}
	if hidden > 0 {
		b.WriteString(fmt.Sprintf("... earlier %d segment(s)\n", hidden))
	}

	for idx, seg := range segments {
		if idx > 0 {
			b.WriteByte('\n')
		}
		title := resolveSegmentTitle(seg, finalize || seg.Closed)
		if finalize || seg.Closed {
			b.WriteString("✓ ")
			b.WriteString(title)
			if summary := summarizeEntries(seg.Entries, progressMaxSummaryItems); summary != "" {
				b.WriteString("\n  ")
				b.WriteString(summary)
			}
			continue
		}

		b.WriteString("… ")
		b.WriteString(title)
		for _, line := range renderActiveEntries(seg.Entries) {
			b.WriteString("\n")
			b.WriteString(line)
		}
	}
	return strings.TrimSpace(b.String())
}

func formatProgressSegment(seg TelegramProgressSegment, completed bool) string {
	title := resolveSegmentTitle(seg, completed)
	if strings.TrimSpace(title) == "" {
		return ""
	}

	var b strings.Builder
	if completed {
		b.WriteString("✓ ")
		b.WriteString(title)
		if summary := summarizeEntries(seg.Entries, progressMaxSummaryItems); summary != "" {
			b.WriteString("\n  ")
			b.WriteString(summary)
		}
		return strings.TrimSpace(b.String())
	}

	b.WriteString("… ")
	b.WriteString(title)
	for _, line := range renderActiveEntries(seg.Entries) {
		b.WriteString("\n")
		b.WriteString(line)
	}
	return strings.TrimSpace(b.String())
}

func (t *TelegramProgressTracker) visibleSegmentsLocked(finalize bool) []TelegramProgressSegment {
	out := append([]TelegramProgressSegment(nil), t.segments...)
	if t.current != nil && len(t.current.Entries) > 0 {
		current := *t.current
		current.Entries = append([]ProgressEntry(nil), t.current.Entries...)
		if finalize {
			current.Closed = true
		}
		out = append(out, current)
	}
	return out
}

func renderActiveEntries(entries []ProgressEntry) []string {
	if len(entries) == 0 {
		return []string{"  " + progressFallbackSummary}
	}
	visible := entries
	if len(visible) > progressMaxItemsPerSeg {
		visible = visible[len(visible)-progressMaxItemsPerSeg:]
	}
	lines := make([]string, 0, len(visible))
	for _, entry := range visible {
		prefix := "  … "
		if entry.Done {
			prefix = "  ✓ "
		}
		line := prefix + entry.DisplayName
		if strings.TrimSpace(entry.Brief) != "" {
			line += ": " + entry.Brief
		}
		lines = append(lines, line)
	}
	return lines
}

func summarizeEntries(entries []ProgressEntry, limit int) string {
	if len(entries) == 0 {
		return ""
	}
	type bucket struct {
		label string
		count int
	}
	buckets := []bucket{}
	indexByLabel := map[string]int{}
	for _, entry := range entries {
		label := strings.TrimSpace(entry.DisplayName)
		if label == "" {
			label = strings.TrimSpace(entry.ToolName)
		}
		if label == "" {
			continue
		}
		if idx, ok := indexByLabel[label]; ok {
			buckets[idx].count++
			continue
		}
		indexByLabel[label] = len(buckets)
		buckets = append(buckets, bucket{label: label, count: 1})
	}
	if len(buckets) == 0 {
		return ""
	}
	if limit > 0 && len(buckets) > limit {
		buckets = buckets[:limit]
	}
	parts := make([]string, 0, len(buckets))
	for _, bucket := range buckets {
		if bucket.count > 1 {
			parts = append(parts, fmt.Sprintf("%s x%d", bucket.label, bucket.count))
			continue
		}
		parts = append(parts, bucket.label)
	}
	return strings.Join(parts, " · ")
}

func resolveSegmentTitle(seg TelegramProgressSegment, completed bool) string {
	if title := strings.TrimSpace(seg.Title); title != "" {
		return title
	}
	if label := strings.TrimSpace(seg.RunSegment.Label); label != "" {
		return label
	}
	stepCount := segmentEffectiveStepCount(seg)
	if completed {
		if stepCount > 0 {
			return formatCompletedStepCount(stepCount)
		}
		return progressCompletedTitle
	}
	if stepCount > 0 {
		return progressLiveProgress
	}
	return progressFallbackSummary
}

func segmentEffectiveStepCount(seg TelegramProgressSegment) int {
	return len(seg.Entries)
}

func formatCompletedStepCount(count int) string {
	if count <= 0 {
		return progressCompletedTitle
	}
	if count == 1 {
		return "1 step completed"
	}
	return fmt.Sprintf("%d steps completed", count)
}

func isHiddenTelegramProgressTool(toolName string) bool {
	switch canonicalToolName(toolName) {
	case "timeline_title", "telegram_reply", "telegram_react":
		return true
	default:
		return false
	}
}

func canonicalToolName(toolName string) string {
	toolName = strings.TrimSpace(strings.ToLower(toolName))
	if toolName == "" {
		return ""
	}
	toolName = strings.ReplaceAll(toolName, "-", "_")
	switch {
	case toolName == "websearch":
		return "web_search"
	case strings.HasPrefix(toolName, "web_search."):
		return "web_search"
	case strings.HasPrefix(toolName, "read."):
		return "read"
	default:
		if idx := strings.Index(toolName, "."); idx > 0 {
			return toolName[:idx]
		}
		return toolName
	}
}

func displayToolName(toolName string) string {
	switch canonicalToolName(toolName) {
	case "timeline_title":
		return "Timeline"
	case "web_search":
		return "Web Search"
	case "memory_search":
		return "Memory Search"
	case "memory_read":
		return "Read Memory"
	case "memory_write":
		return "Write Memory"
	case "memory_edit":
		return "Edit Memory"
	case "memory_forget":
		return "Forget Memory"
	case "arkloop_help":
		return "Arkloop Help"
	case "notebook_read":
		return "Read Notebook"
	case "notebook_write":
		return "Write Notebook"
	case "notebook_edit":
		return "Edit Notebook"
	case "notebook_forget":
		return "Forget Notebook"
	case "code_interpreter", "python_execute", "exec_command", "continue_process", "terminate_process":
		return "Code Execution"
	case "read_file", "read":
		return "Read File"
	case "write_file", "edit", "edit_file":
		return "Edit File"
	case "load_tools":
		return "Load Tools"
	case "spawn_agent", "acp_agent", "spawn_acp", "sub_agent":
		return "Sub-agent"
	case "browser":
		return "Browser"
	case "web_fetch", "fetch_url":
		return "Web Fetch"
	default:
		name := canonicalToolName(toolName)
		if name == "" {
			return "Tool"
		}
		parts := strings.Split(name, "_")
		for i := range parts {
			if parts[i] == "" {
				continue
			}
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
		return strings.Join(parts, " ")
	}
}

func toolBrief(toolName, argsJSON string) string {
	if strings.TrimSpace(argsJSON) == "" {
		return ""
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return ""
	}
	switch canonicalToolName(toolName) {
	case "arkloop_help":
		return truncateBrief(extractStringField(args, "query"))
	case "memory_search":
		return truncateBrief(extractStringField(args, "query"))
	case "web_search":
		if query := truncateBrief(extractStringField(args, "query")); query != "" {
			return query
		}
		if queries := extractStringSliceField(args, "queries"); len(queries) > 0 {
			return truncateBrief(queries[0])
		}
		return ""
	case "memory_write", "memory_edit", "notebook_read", "notebook_write", "notebook_edit", "notebook_forget":
		return truncateBrief(extractStringField(args, "key"))
	case "memory_read":
		if id := truncateBrief(extractStringField(args, "id")); id != "" {
			return id
		}
		return truncateBrief(extractStringField(args, "key"))
	case "memory_forget":
		return truncateBrief(extractStringField(args, "id"))
	case "code_interpreter", "python_execute":
		return "Python"
	case "exec_command", "continue_process", "terminate_process":
		return truncateBrief(extractStringField(args, "cmd"))
	case "read_file", "write_file", "edit", "edit_file", "read":
		return truncateBrief(extractStringField(args, "path"))
	case "spawn_agent", "acp_agent", "spawn_acp", "sub_agent":
		return truncateBrief(extractStringField(args, "task"))
	default:
		return ""
	}
}

func extractStringField(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok {
		return ""
	}
	switch val := v.(type) {
	case string:
		return strings.TrimSpace(val)
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case int:
		return strconv.Itoa(val)
	default:
		return fmt.Sprintf("%v", val)
	}
}

func extractStringSliceField(args map[string]any, key string) []string {
	raw, ok := args[key]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s := strings.TrimSpace(progressStringValue(item))
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func truncateBrief(s string) string {
	const maxLen = 60
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func progressStringValue(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case fmt.Stringer:
		return val.String()
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case int:
		return strconv.Itoa(val)
	default:
		return fmt.Sprintf("%v", val)
	}
}

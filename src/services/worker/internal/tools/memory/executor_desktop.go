//go:build desktop

package memory

import (
	"context"
	"fmt"
	"strings"
	"time"

	datarepo "arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/memory"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
)

type ToolExecutor struct {
	provider  memory.MemoryProvider
	db        datarepo.DesktopDB
	snapshots desktopSnapshotAppender
}

type desktopSnapshotAppender interface {
	AppendMemoryLine(ctx context.Context, pool datarepo.DesktopDB, accountID, userID uuid.UUID, agentID, line string) error
	Invalidate(ctx context.Context, pool datarepo.DesktopDB, accountID, userID uuid.UUID, agentID string) error
}

// NewToolExecutor creates a desktop memory tool executor backed by the given memory provider.
func NewToolExecutor(provider memory.MemoryProvider, db datarepo.DesktopDB, snapshots desktopSnapshotAppender) *ToolExecutor {
	if snapshots == nil {
		snapshots = datarepo.MemorySnapshotRepository{}
	}
	return &ToolExecutor{provider: provider, db: db, snapshots: snapshots}
}

func (e *ToolExecutor) Execute(
	ctx context.Context,
	toolName string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	_ string,
) tools.ExecutionResult {
	started := time.Now()

	ident, err := buildIdentity(execCtx)
	if err != nil {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: errorIdentityMissing,
				Message:    err.Error(),
			},
			DurationMs: durationMs(started),
		}
	}

	switch toolName {
	case "notebook_read":
		return e.notebookRead(ctx, args, ident, started)
	case "notebook_write":
		return e.notebookWrite(ctx, args, ident, started)
	case "notebook_edit":
		return e.notebookEdit(ctx, args, ident, started)
	case "notebook_forget":
		return e.notebookForget(ctx, args, ident, started)
	case "memory_search":
		return e.search(ctx, args, ident, started)
	case "memory_thread_search":
		return e.threadSearch(ctx, args, ident, started)
	case "memory_thread_fetch":
		return e.threadFetch(ctx, args, ident, started)
	case "memory_read":
		return e.read(ctx, args, ident, started)
	case "memory_write":
		return e.write(ctx, args, ident, execCtx, started)
	case "memory_edit":
		return e.edit(ctx, args, ident, execCtx, started)
	case "memory_forget":
		return e.forget(ctx, args, ident, execCtx, started)
	default:
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: "tool.not_registered",
				Message:    "unknown memory tool: " + toolName,
			},
			DurationMs: durationMs(started),
		}
	}
}

func (e *ToolExecutor) notebookRead(ctx context.Context, args map[string]any, ident memory.MemoryIdentity, started time.Time) tools.ExecutionResult {
	uri, _ := args["uri"].(string)
	uri = strings.TrimSpace(uri)
	if uri == "" {
		if getter, ok := e.provider.(interface {
			GetSnapshot(context.Context, uuid.UUID, uuid.UUID, string) (string, error)
		}); ok {
			content, err := getter.GetSnapshot(ctx, ident.AccountID, ident.UserID, ident.AgentID)
			if err != nil {
				return providerError("notebook_read", err, started)
			}
			return tools.ExecutionResult{
				ResultJSON: map[string]any{"content": content},
				DurationMs: durationMs(started),
			}
		}
		return tools.ExecutionResult{
			ResultJSON: map[string]any{"content": ""},
			DurationMs: durationMs(started),
		}
	}

	content, err := e.provider.Content(ctx, ident, uri, memory.MemoryLayerRead)
	if err != nil {
		return providerError("notebook_read", err, started)
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{"content": content},
		DurationMs: durationMs(started),
	}
}

func (e *ToolExecutor) notebookWrite(ctx context.Context, args map[string]any, ident memory.MemoryIdentity, started time.Time) tools.ExecutionResult {
	w, ok := e.provider.(memory.DesktopLocalMemoryWriteURI)
	if !ok {
		return stateError("notebook is not available in this runtime", started)
	}
	category, key, content, err := parseNotebookArgs(args)
	if err != nil {
		return argError(err.Error(), started)
	}
	entry := memory.MemoryEntry{Content: buildWritableContent(memory.MemoryScopeUser, category, key, content)}
	uri, writeErr := w.WriteReturningURI(ctx, ident, memory.MemoryScopeUser, entry)
	if writeErr != nil {
		return providerError("notebook_write", writeErr, started)
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{"status": "ok", "uri": uri},
		DurationMs: durationMs(started),
	}
}

func (e *ToolExecutor) notebookEdit(ctx context.Context, args map[string]any, ident memory.MemoryIdentity, started time.Time) tools.ExecutionResult {
	editor, ok := e.provider.(memory.DesktopLocalMemoryEditURI)
	if !ok {
		return stateError("notebook editing is not available in this runtime", started)
	}
	uri, ok := args["uri"].(string)
	if !ok || strings.TrimSpace(uri) == "" {
		return argError("uri must be a non-empty string", started)
	}
	category, key, content, err := parseNotebookArgs(args)
	if err != nil {
		return argError(err.Error(), started)
	}
	entry := memory.MemoryEntry{Content: buildWritableContent(memory.MemoryScopeUser, category, key, content)}
	if err := editor.UpdateByURI(ctx, ident, strings.TrimSpace(uri), entry); err != nil {
		return providerError("notebook_edit", err, started)
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{"status": "ok", "uri": strings.TrimSpace(uri)},
		DurationMs: durationMs(started),
	}
}

func (e *ToolExecutor) notebookForget(ctx context.Context, args map[string]any, ident memory.MemoryIdentity, started time.Time) tools.ExecutionResult {
	uri, ok := args["uri"].(string)
	if !ok || strings.TrimSpace(uri) == "" {
		return argError("uri must be a non-empty string", started)
	}
	if err := e.provider.Delete(ctx, ident, strings.TrimSpace(uri)); err != nil {
		return providerError("notebook_forget", err, started)
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{"status": "ok"},
		DurationMs: durationMs(started),
	}
}

func (e *ToolExecutor) search(ctx context.Context, args map[string]any, ident memory.MemoryIdentity, started time.Time) tools.ExecutionResult {
	query, ok := args["query"].(string)
	if !ok || strings.TrimSpace(query) == "" {
		return argError("query must be a non-empty string", started)
	}

	limit := parseLimit(args, defaultSearchLimit)

	hits, err := e.provider.Find(ctx, ident, memory.SelfURI(ident.UserID.String()), query, limit)
	if err != nil {
		return providerError("search", err, started)
	}

	results := make([]map[string]any, 0, len(hits))
	for _, h := range hits {
		results = append(results, map[string]any{
			"uri":      h.URI,
			"abstract": h.Abstract,
		})
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{"hits": results},
		DurationMs: durationMs(started),
	}
}

func (e *ToolExecutor) read(ctx context.Context, args map[string]any, ident memory.MemoryIdentity, started time.Time) tools.ExecutionResult {
	uri, ok := args["uri"].(string)
	if !ok || strings.TrimSpace(uri) == "" {
		return argError("uri must be a non-empty string", started)
	}

	layer := memory.MemoryLayerOverview
	if depth, ok := args["depth"].(string); ok && depth == "full" {
		layer = memory.MemoryLayerRead
	}

	content, err := e.provider.Content(ctx, ident, uri, layer)
	if err != nil {
		return providerError("read", err, started)
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{"content": content},
		DurationMs: durationMs(started),
	}
}

func (e *ToolExecutor) threadSearch(ctx context.Context, args map[string]any, ident memory.MemoryIdentity, started time.Time) tools.ExecutionResult {
	provider, ok := e.provider.(memory.MemoryThreadProvider)
	if !ok {
		return stateError("thread search is not available in this runtime", started)
	}
	query, ok := args["query"].(string)
	if !ok || strings.TrimSpace(query) == "" {
		return argError("query must be a non-empty string", started)
	}
	limit := parseLimit(args, defaultSearchLimit)
	data, err := provider.SearchThreads(ctx, ident, strings.TrimSpace(query), limit)
	if err != nil {
		return providerError("thread_search", err, started)
	}
	return tools.ExecutionResult{ResultJSON: data, DurationMs: durationMs(started)}
}

func (e *ToolExecutor) threadFetch(ctx context.Context, args map[string]any, ident memory.MemoryIdentity, started time.Time) tools.ExecutionResult {
	provider, ok := e.provider.(memory.MemoryThreadProvider)
	if !ok {
		return stateError("thread fetch is not available in this runtime", started)
	}
	threadID, ok := args["thread_id"].(string)
	if !ok || strings.TrimSpace(threadID) == "" {
		return argError("thread_id must be a non-empty string", started)
	}
	offset := 0
	if raw, ok := args["offset"].(float64); ok && raw > 0 {
		offset = int(raw)
	}
	limit := 50
	if raw, ok := args["limit"].(float64); ok && raw > 0 {
		limit = int(raw)
	}
	data, err := provider.FetchThread(ctx, ident, strings.TrimSpace(threadID), offset, limit)
	if err != nil {
		return providerError("thread_fetch", err, started)
	}
	return tools.ExecutionResult{ResultJSON: data, DurationMs: durationMs(started)}
}

func (e *ToolExecutor) write(ctx context.Context, args map[string]any, ident memory.MemoryIdentity, execCtx tools.ExecutionContext, started time.Time) tools.ExecutionResult {
	category, ok := args["category"].(string)
	if !ok || strings.TrimSpace(category) == "" {
		return argError("category must be a non-empty string", started)
	}
	key, ok := args["key"].(string)
	if !ok || strings.TrimSpace(key) == "" {
		return argError("key must be a non-empty string", started)
	}
	content, ok := args["content"].(string)
	if !ok || strings.TrimSpace(content) == "" {
		return argError("content must be a non-empty string", started)
	}

	scope := parseScope(args)
	writable := buildWritableContent(scope, category, key, content)
	entry := memory.MemoryEntry{Content: writable}

	if w, ok := e.provider.(memory.DesktopLocalMemoryWriteURI); ok {
		uri, err := w.WriteReturningURI(ctx, ident, scope, entry)
		if err != nil {
			return providerError("write", err, started)
		}
		return tools.ExecutionResult{
			ResultJSON: map[string]any{"status": "ok", "uri": uri},
			DurationMs: durationMs(started),
		}
	}

	if execCtx.PendingMemoryWrites == nil {
		return stateError("pending memory buffer not available", started)
	}
	taskID := uuid.NewString()
	execCtx.PendingMemoryWrites.Append(memory.PendingWrite{
		TaskID: taskID,
		Ident:  ident,
		Scope:  scope,
		Entry:  entry,
	})
	queued := execCtx.Emitter.Emit("memory.write.queued", map[string]any{
		"task_id":          taskID,
		"scope":            string(scope),
		"agent_id":         ident.AgentID,
		"snapshot_updated": false,
	}, stringPtr("memory_write"), nil)
	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"status":           "queued",
			"task_id":          taskID,
			"snapshot_updated": false,
		},
		DurationMs: durationMs(started),
		Events:     []events.RunEvent{queued},
	}
}

func (e *ToolExecutor) forget(ctx context.Context, args map[string]any, ident memory.MemoryIdentity, execCtx tools.ExecutionContext, started time.Time) tools.ExecutionResult {
	uri, ok := args["uri"].(string)
	if !ok || strings.TrimSpace(uri) == "" {
		return argError("uri must be a non-empty string", started)
	}

	if err := e.provider.Delete(ctx, ident, uri); err != nil {
		return providerError("forget", err, started)
	}
	if _, local := e.provider.(memory.DesktopLocalMemoryWriteURI); !local && e.db != nil {
		pipeline.ForgetSnapshotRefresh(e.provider, pipeline.NewDesktopMemorySnapshotStore(e.db), e.db, execCtx.RunID, execCtx.TraceID, ident)
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{"status": "ok"},
		DurationMs: durationMs(started),
	}
}

func (e *ToolExecutor) edit(ctx context.Context, args map[string]any, ident memory.MemoryIdentity, execCtx tools.ExecutionContext, started time.Time) tools.ExecutionResult {
	editor, ok := e.provider.(memory.MemoryEditURI)
	if !ok {
		return stateError("memory editing is not available in this runtime", started)
	}
	uri, ok := args["uri"].(string)
	if !ok || strings.TrimSpace(uri) == "" {
		return argError("uri must be a non-empty string", started)
	}
	content, ok := args["content"].(string)
	if !ok || strings.TrimSpace(content) == "" {
		return argError("content must be a non-empty string", started)
	}
	if err := editor.UpdateByURI(ctx, ident, strings.TrimSpace(uri), memory.MemoryEntry{Content: strings.TrimSpace(content)}); err != nil {
		return providerError("edit", err, started)
	}
	if _, local := e.provider.(memory.DesktopLocalMemoryWriteURI); !local && e.db != nil {
		pipeline.EditSnapshotRefresh(e.provider, pipeline.NewDesktopMemorySnapshotStore(e.db), e.db, execCtx.RunID, execCtx.TraceID, ident, strings.TrimSpace(content))
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{"status": "ok", "uri": strings.TrimSpace(uri)},
		DurationMs: durationMs(started),
	}
}

// ---------- shared helpers (duplicated from executor.go to avoid build-tag conflicts) ----------

const (
	errorArgsInvalid     = "tool.args_invalid"
	errorProviderFailure = "tool.memory_provider_failure"
	errorIdentityMissing = "tool.memory_identity_missing"
	errorStateMissing    = "tool.memory_state_missing"
	errorSnapshotFailed  = "tool.memory_snapshot_failed"

	defaultSearchLimit = 5
)

func buildIdentity(execCtx tools.ExecutionContext) (memory.MemoryIdentity, error) {
	if execCtx.UserID == nil {
		return memory.MemoryIdentity{}, fmt.Errorf("user_id not available, memory operations require authenticated user")
	}
	accountID := uuid.Nil
	if execCtx.AccountID != nil {
		accountID = *execCtx.AccountID
	}
	return memory.MemoryIdentity{
		AccountID: accountID,
		UserID:    *execCtx.UserID,
		AgentID:   "user_" + execCtx.UserID.String(),
	}, nil
}

func parseScope(args map[string]any) memory.MemoryScope {
	if s, ok := args["scope"].(string); ok {
		// User is the only long-term subject. Keep "agent" as a tolerated input.
		if strings.EqualFold(strings.TrimSpace(s), "user") || strings.EqualFold(strings.TrimSpace(s), "agent") {
			return memory.MemoryScopeUser
		}
	}
	return memory.MemoryScopeUser
}

func parseLimit(args map[string]any, fallback int) int {
	switch v := args["limit"].(type) {
	case float64:
		if n := int(v); n >= 1 && n <= 20 {
			return n
		}
	case int:
		if v >= 1 && v <= 20 {
			return v
		}
	case int64:
		if v >= 1 && v <= 20 {
			return int(v)
		}
	}
	return fallback
}

func buildWritableContent(scope memory.MemoryScope, category, key, content string) string {
	return "[" + string(scope) + "/" + category + "/" + key + "] " + content
}

func parseNotebookArgs(args map[string]any) (string, string, string, error) {
	category, ok := args["category"].(string)
	if !ok || strings.TrimSpace(category) == "" {
		return "", "", "", fmt.Errorf("category must be a non-empty string")
	}
	key, ok := args["key"].(string)
	if !ok || strings.TrimSpace(key) == "" {
		return "", "", "", fmt.Errorf("key must be a non-empty string")
	}
	content, ok := args["content"].(string)
	if !ok || strings.TrimSpace(content) == "" {
		return "", "", "", fmt.Errorf("content must be a non-empty string")
	}
	return strings.TrimSpace(category), strings.TrimSpace(key), strings.TrimSpace(content), nil
}

func argError(msg string, started time.Time) tools.ExecutionResult {
	return tools.ExecutionResult{
		Error:      &tools.ExecutionError{ErrorClass: errorArgsInvalid, Message: msg},
		DurationMs: durationMs(started),
	}
}

func stateError(msg string, started time.Time) tools.ExecutionResult {
	return tools.ExecutionResult{
		Error:      &tools.ExecutionError{ErrorClass: errorStateMissing, Message: msg},
		DurationMs: durationMs(started),
	}
}

func snapshotError(err error, started time.Time) tools.ExecutionResult {
	return tools.ExecutionResult{
		Error: &tools.ExecutionError{
			ErrorClass: errorSnapshotFailed,
			Message:    "memory snapshot update failed: " + err.Error(),
		},
		DurationMs: durationMs(started),
	}
}

func providerError(op string, err error, started time.Time) tools.ExecutionResult {
	return tools.ExecutionResult{
		Error: &tools.ExecutionError{
			ErrorClass: errorProviderFailure,
			Message:    "memory " + op + " failed: " + err.Error(),
		},
		DurationMs: durationMs(started),
	}
}

func durationMs(started time.Time) int {
	ms := int(time.Since(started) / time.Millisecond)
	if ms < 0 {
		return 0
	}
	return ms
}

//go:build desktop

package memory

import (
	"context"
	"fmt"
	"strings"
	"time"

	"arkloop/services/worker/internal/memory"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
)

// ToolExecutor handles the four memory tools on Desktop using any MemoryProvider.
// For local mode this is the SQLite-backed provider; for OpenViking mode it is
// the HTTP client provider. No pending-write buffer is used.
type ToolExecutor struct {
	provider memory.MemoryProvider
}

// NewToolExecutor creates a desktop memory tool executor backed by the given
// memory provider.
func NewToolExecutor(provider memory.MemoryProvider) *ToolExecutor {
	return &ToolExecutor{provider: provider}
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
	case "memory_search":
		return e.search(ctx, args, ident, started)
	case "memory_read":
		return e.read(ctx, args, ident, started)
	case "memory_write":
		return e.write(ctx, args, ident, started)
	case "memory_forget":
		return e.forget(ctx, args, ident, started)
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

func (e *ToolExecutor) search(ctx context.Context, args map[string]any, ident memory.MemoryIdentity, started time.Time) tools.ExecutionResult {
	query, ok := args["query"].(string)
	if !ok || strings.TrimSpace(query) == "" {
		return argError("query must be a non-empty string", started)
	}

	scope := memory.SearchScopeForDesktopLocal(args)
	limit := parseLimit(args, defaultSearchLimit)

	hits, err := e.provider.Find(ctx, ident, scope, query, limit)
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

func (e *ToolExecutor) write(ctx context.Context, args map[string]any, ident memory.MemoryIdentity, started time.Time) tools.ExecutionResult {
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

	if err := e.provider.Write(ctx, ident, scope, entry); err != nil {
		return providerError("write", err, started)
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{"status": "ok"},
		DurationMs: durationMs(started),
	}
}

func (e *ToolExecutor) forget(ctx context.Context, args map[string]any, ident memory.MemoryIdentity, started time.Time) tools.ExecutionResult {
	uri, ok := args["uri"].(string)
	if !ok || strings.TrimSpace(uri) == "" {
		return argError("uri must be a non-empty string", started)
	}

	if err := e.provider.Delete(ctx, ident, uri); err != nil {
		return providerError("forget", err, started)
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{"status": "ok"},
		DurationMs: durationMs(started),
	}
}

// ---------- shared helpers (duplicated from executor.go to avoid build-tag conflicts) ----------

const (
	errorArgsInvalid     = "tool.args_invalid"
	errorProviderFailure = "tool.memory_provider_failure"
	errorIdentityMissing = "tool.memory_identity_missing"

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
	// Desktop local memory is user-level: all personas share a single bucket.
	// Using a fixed agent_id ensures memories written by any persona are visible
	// to all other personas and to the settings UI.
	return memory.MemoryIdentity{
		AccountID: accountID,
		UserID:    *execCtx.UserID,
		AgentID:   "default",
	}, nil
}

func parseScope(args map[string]any) memory.MemoryScope {
	if s, ok := args["scope"].(string); ok && s == "agent" {
		return memory.MemoryScopeAgent
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

func argError(msg string, started time.Time) tools.ExecutionResult {
	return tools.ExecutionResult{
		Error: &tools.ExecutionError{ErrorClass: errorArgsInvalid, Message: msg},
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

//go:build desktop

package memory

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	sharedconfig "arkloop/services/shared/config"
	datarepo "arkloop/services/worker/internal/data"
	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/memory"
	"arkloop/services/worker/internal/memory/nowledge"
	"arkloop/services/worker/internal/pipeline"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
)

type ToolExecutor struct {
	provider       memory.MemoryProvider
	db             datarepo.DesktopDB
	snapshots      desktopSnapshotAppender
	impStore       pipeline.ImpressionStore
	impRefresh     pipeline.ImpressionRefreshFunc
	configResolver sharedconfig.Resolver
}

type desktopSnapshotAppender interface {
	AppendMemoryLine(ctx context.Context, pool datarepo.DesktopDB, accountID, userID uuid.UUID, agentID, line string) error
	Invalidate(ctx context.Context, pool datarepo.DesktopDB, accountID, userID uuid.UUID, agentID string) error
}

type richSearchProvider interface {
	SearchRich(ctx context.Context, ident memory.MemoryIdentity, query string, limit int) ([]nowledge.SearchResult, error)
}

type graphProvider interface {
	Connections(ctx context.Context, ident memory.MemoryIdentity, memoryID string, depth, limit int) ([]nowledge.GraphConnection, error)
	Timeline(ctx context.Context, ident memory.MemoryIdentity, lastNDays int, dateFrom, dateTo, eventType string, tier1Only bool, limit int) ([]nowledge.TimelineEvent, error)
}

type detailedReadProvider interface {
	MemoryDetail(ctx context.Context, ident memory.MemoryIdentity, uri string) (nowledge.MemoryDetail, error)
	ReadWorkingMemory(ctx context.Context, ident memory.MemoryIdentity) (nowledge.WorkingMemory, error)
}

type snippetReadProvider interface {
	MemorySnippet(ctx context.Context, ident memory.MemoryIdentity, uri string, fromLine, lineCount int) (nowledge.MemorySnippet, error)
	ReadWorkingMemory(ctx context.Context, ident memory.MemoryIdentity) (nowledge.WorkingMemory, error)
}

type workingMemoryContextProvider interface {
	ReadWorkingMemory(ctx context.Context, ident memory.MemoryIdentity) (nowledge.WorkingMemory, error)
	PatchWorkingMemory(ctx context.Context, ident memory.MemoryIdentity, heading string, patch nowledge.WorkingMemoryPatch) (nowledge.WorkingMemory, error)
}

type statusProvider interface {
	Status(ctx context.Context, ident memory.MemoryIdentity) (nowledge.Status, error)
}

type threadSearchFilterProvider interface {
	SearchThreadsFull(ctx context.Context, ident memory.MemoryIdentity, query string, limit int, source string) (map[string]any, error)
}

// NewToolExecutor creates a desktop memory tool executor backed by the given memory provider.
func NewToolExecutor(provider memory.MemoryProvider, db datarepo.DesktopDB, snapshots desktopSnapshotAppender) *ToolExecutor {
	if snapshots == nil {
		snapshots = datarepo.MemorySnapshotRepository{}
	}
	return &ToolExecutor{provider: provider, db: db, snapshots: snapshots}
}

func (e *ToolExecutor) ConfigureImpression(store pipeline.ImpressionStore, refresh pipeline.ImpressionRefreshFunc, resolver sharedconfig.Resolver) {
	if e == nil {
		return
	}
	e.impStore = store
	e.impRefresh = refresh
	e.configResolver = resolver
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
	case "memory_connections":
		return e.connections(ctx, args, ident, started)
	case "memory_timeline":
		return e.timeline(ctx, args, ident, started)
	case "memory_context":
		return e.context(ctx, args, ident, started)
	case "memory_status":
		return e.status(ctx, ident, started)
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

	results, err := e.searchResults(ctx, ident, query, limit)
	if err != nil {
		return providerError("search", err, started)
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{"hits": results},
		DurationMs: durationMs(started),
	}
}

func (e *ToolExecutor) searchResults(ctx context.Context, ident memory.MemoryIdentity, query string, limit int) ([]map[string]any, error) {
	if provider, ok := e.provider.(richSearchProvider); ok {
		results, err := provider.SearchRich(ctx, ident, query, limit)
		if err != nil {
			return nil, err
		}
		hits := make([]map[string]any, 0, len(results))
		for _, item := range results {
			abstract := strings.TrimSpace(item.Title)
			if abstract == "" {
				abstract = strings.TrimSpace(item.Content)
			}
			kind := strings.TrimSpace(item.Kind)
			if kind == "" {
				kind = "memory"
			}
			uri := "nowledge://memory/" + item.ID
			if kind == "thread" {
				threadID := strings.TrimSpace(item.ThreadID)
				if threadID == "" {
					threadID = strings.TrimSpace(item.ID)
				}
				if threadID != "" {
					uri = "nowledge://thread/" + threadID
				}
			}
			hit := map[string]any{
				"uri":      uri,
				"abstract": abstract,
				"kind":     kind,
				"score":    item.Score,
			}
			if strings.TrimSpace(item.RelevanceReason) != "" {
				hit["matched_via"] = strings.TrimSpace(item.RelevanceReason)
			}
			if strings.TrimSpace(item.SourceThreadID) != "" {
				hit["source_thread_id"] = strings.TrimSpace(item.SourceThreadID)
			}
			if strings.TrimSpace(item.ThreadID) != "" {
				hit["thread_id"] = strings.TrimSpace(item.ThreadID)
			}
			if strings.TrimSpace(item.MatchedSnippet) != "" {
				hit["matched_snippet"] = strings.TrimSpace(item.MatchedSnippet)
			}
			if len(item.Snippets) > 0 {
				hit["snippets"] = append([]string(nil), item.Snippets...)
			}
			if item.Importance != 0 {
				hit["importance"] = item.Importance
			}
			if len(item.Labels) > 0 {
				hit["labels"] = append([]string(nil), item.Labels...)
			}
			if len(item.RelatedThreads) > 0 {
				related := make([]map[string]any, 0, len(item.RelatedThreads))
				for _, thread := range item.RelatedThreads {
					related = append(related, map[string]any{
						"thread_id":       thread.ThreadID,
						"title":           thread.Title,
						"source":          thread.Source,
						"message_count":   thread.MessageCount,
						"score":           thread.Score,
						"matched_snippet": thread.MatchedSnippet,
						"snippets":        append([]string(nil), thread.Snippets...),
					})
				}
				hit["related_threads"] = related
			}
			hits = append(hits, hit)
		}
		sort.SliceStable(hits, func(i, j int) bool {
			si, _ := hits[i]["score"].(float64)
			sj, _ := hits[j]["score"].(float64)
			return si > sj
		})
		if limit > 0 && len(hits) > limit {
			hits = hits[:limit]
		}
		return hits, nil
	}

	hits, err := e.provider.Find(ctx, ident, memory.SelfURI(ident.UserID.String()), query, limit)
	if err != nil {
		return nil, err
	}
	results := make([]map[string]any, 0, len(hits))
	for _, h := range hits {
		results = append(results, map[string]any{
			"uri":      h.URI,
			"abstract": h.Abstract,
			"kind":     "memory",
			"score":    h.Score,
		})
	}
	return results, nil
}

func (e *ToolExecutor) read(ctx context.Context, args map[string]any, ident memory.MemoryIdentity, started time.Time) tools.ExecutionResult {
	uri, ok := args["uri"].(string)
	if !ok || strings.TrimSpace(uri) == "" {
		return argError("uri must be a non-empty string", started)
	}
	depth := "overview"
	if value, ok := args["depth"].(string); ok && strings.EqualFold(strings.TrimSpace(value), "full") {
		depth = "full"
	}
	fromLine := 0
	if raw, ok := args["from"].(float64); ok && raw >= 1 {
		fromLine = int(raw)
	}
	lineCount := 0
	if raw, ok := args["lines"].(float64); ok && raw >= 1 {
		lineCount = int(raw)
	}
	wantsSnippet := fromLine > 0 || lineCount > 0
	if provider, ok := e.provider.(detailedReadProvider); ok {
		normalizedURI := strings.TrimSpace(uri)
		lowerURI := strings.ToLower(normalizedURI)
		if lowerURI == "memory.md" || lowerURI == "memory" {
			wm, err := provider.ReadWorkingMemory(ctx, ident)
			if err != nil {
				return providerError("read", err, started)
			}
			result := map[string]any{
				"content": summarizeMemoryReadDesktop("", wm.Content, depth),
				"source":  "working_memory",
			}
			if wantsSnippet {
				content, startLine, endLine, totalLines := sliceReadLinesDesktop(wm.Content, fromLine, lineCount)
				result["content"] = content
				result["start_line"] = startLine
				result["end_line"] = endLine
				result["total_lines"] = totalLines
			}
			return tools.ExecutionResult{
				ResultJSON: result,
				DurationMs: durationMs(started),
			}
		}
		if strings.HasPrefix(normalizedURI, "nowledge://memory/") {
			if wantsSnippet {
				snippetProvider, ok := e.provider.(snippetReadProvider)
				if !ok {
					return stateError("memory line slicing is not available in this runtime", started)
				}
				snippet, err := snippetProvider.MemorySnippet(ctx, ident, normalizedURI, fromLine, lineCount)
				if err != nil {
					return providerError("read", err, started)
				}
				result := map[string]any{
					"content":     strings.TrimSpace(snippet.Text),
					"start_line":  snippet.StartLine,
					"end_line":    snippet.EndLine,
					"total_lines": snippet.TotalLines,
				}
				if strings.TrimSpace(snippet.Title) != "" {
					result["title"] = strings.TrimSpace(snippet.Title)
				}
				if strings.TrimSpace(snippet.SourceThreadID) != "" {
					result["source_thread_id"] = strings.TrimSpace(snippet.SourceThreadID)
				}
				return tools.ExecutionResult{ResultJSON: result, DurationMs: durationMs(started)}
			}
			detail, err := provider.MemoryDetail(ctx, ident, normalizedURI)
			if err != nil {
				return providerError("read", err, started)
			}
			result := map[string]any{"content": summarizeMemoryReadDesktop(detail.Title, detail.Content, depth)}
			if strings.TrimSpace(detail.SourceThreadID) != "" {
				result["source_thread_id"] = strings.TrimSpace(detail.SourceThreadID)
			}
			return tools.ExecutionResult{ResultJSON: result, DurationMs: durationMs(started)}
		}
	}
	if strings.HasPrefix(strings.TrimSpace(uri), "nowledge://thread/") {
		threadProvider, ok := e.provider.(memory.MemoryThreadProvider)
		if !ok {
			return stateError("thread fetch is not available in this runtime", started)
		}
		threadID := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(uri), "nowledge://thread/"))
		if threadID == "" {
			return argError("uri must include a valid nowledge thread id", started)
		}
		data, err := threadProvider.FetchThread(ctx, ident, threadID, 0, 50)
		if err != nil {
			return providerError("read", err, started)
		}
		result := map[string]any{
			"content":   renderThreadReadContentDesktop(data, depth),
			"thread_id": threadID,
			"source":    "thread",
		}
		if title, _ := data["title"].(string); strings.TrimSpace(title) != "" {
			result["title"] = strings.TrimSpace(title)
		}
		return tools.ExecutionResult{ResultJSON: result, DurationMs: durationMs(started)}
	}
	if wantsSnippet {
		return stateError("memory line slicing is not available in this runtime", started)
	}

	layer := memory.MemoryLayerOverview
	if depth == "full" {
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

func (e *ToolExecutor) connections(ctx context.Context, args map[string]any, ident memory.MemoryIdentity, started time.Time) tools.ExecutionResult {
	provider, ok := e.provider.(graphProvider)
	if !ok {
		return stateError("memory connections are not available in this runtime", started)
	}
	memoryID, _ := args["memory_id"].(string)
	query, _ := args["query"].(string)
	memoryID = strings.TrimSpace(memoryID)
	query = strings.TrimSpace(query)
	if memoryID == "" && query == "" {
		return argError("memory_id or query must be provided", started)
	}
	if memoryID == "" {
		results, err := e.searchResults(ctx, ident, query, 1)
		if err != nil {
			return providerError("connections", err, started)
		}
		if len(results) == 0 {
			return tools.ExecutionResult{ResultJSON: map[string]any{"connections": []map[string]any{}, "memory_id": "", "query": query}, DurationMs: durationMs(started)}
		}
		if uri, _ := results[0]["uri"].(string); strings.TrimSpace(uri) != "" {
			var err error
			memoryID, err = nowledge.MemoryIDFromURI(uri)
			if err != nil {
				return providerError("connections", err, started)
			}
		}
	}
	depth := 1
	if raw, ok := args["depth"].(float64); ok && raw >= 1 {
		depth = int(raw)
	}
	limit := 20
	if raw, ok := args["limit"].(float64); ok && raw >= 1 {
		limit = int(raw)
	}
	connections, err := provider.Connections(ctx, ident, memoryID, depth, limit)
	if err != nil {
		return providerError("connections", err, started)
	}
	out := make([]map[string]any, 0, len(connections))
	for _, item := range connections {
		out = append(out, map[string]any{
			"memory_id":   item.MemoryID,
			"node_id":     item.NodeID,
			"node_type":   item.NodeType,
			"title":       item.Title,
			"snippet":     item.Snippet,
			"edge_type":   item.EdgeType,
			"relation":    item.Relation,
			"weight":      item.Weight,
			"source_type": item.SourceType,
		})
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"memory_id":   memoryID,
			"query":       query,
			"depth":       depth,
			"connections": out,
			"total_found": len(out),
		},
		DurationMs: durationMs(started),
	}
}

func (e *ToolExecutor) timeline(ctx context.Context, args map[string]any, ident memory.MemoryIdentity, started time.Time) tools.ExecutionResult {
	provider, ok := e.provider.(graphProvider)
	if !ok {
		return stateError("memory timeline is not available in this runtime", started)
	}
	lastNDays := 7
	if raw, ok := args["last_n_days"].(float64); ok && raw >= 1 {
		lastNDays = int(raw)
	}
	dateFrom, _ := args["date_from"].(string)
	dateTo, _ := args["date_to"].(string)
	eventType, _ := args["event_type"].(string)
	tier1Only := true
	if raw, ok := args["tier1_only"].(bool); ok {
		tier1Only = raw
	}
	limit := 100
	if raw, ok := args["limit"].(float64); ok && raw >= 1 {
		limit = int(raw)
	}
	events, err := provider.Timeline(ctx, ident, lastNDays, strings.TrimSpace(dateFrom), strings.TrimSpace(dateTo), strings.TrimSpace(eventType), tier1Only, limit)
	if err != nil {
		return providerError("timeline", err, started)
	}
	grouped := make(map[string][]map[string]any)
	dates := make([]string, 0)
	for _, item := range events {
		date := strings.TrimSpace(item.CreatedAt)
		if len(date) >= 10 {
			date = date[:10]
		}
		if date == "" {
			date = "unknown"
		}
		if _, ok := grouped[date]; !ok {
			dates = append(dates, date)
		}
		grouped[date] = append(grouped[date], map[string]any{
			"id":                 item.ID,
			"event_type":         item.EventType,
			"label":              item.Label,
			"title":              item.Title,
			"description":        item.Description,
			"created_at":         item.CreatedAt,
			"memory_id":          item.MemoryID,
			"related_memory_ids": append([]string(nil), item.RelatedMemoryIDs...),
		})
	}
	sort.SliceStable(dates, func(i, j int) bool {
		return dates[i] > dates[j]
	})
	days := make([]map[string]any, 0, len(dates))
	for _, date := range dates {
		days = append(days, map[string]any{
			"date":   date,
			"count":  len(grouped[date]),
			"events": grouped[date],
		})
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"last_n_days": lastNDays,
			"date_from":   strings.TrimSpace(dateFrom),
			"date_to":     strings.TrimSpace(dateTo),
			"event_type":  strings.TrimSpace(eventType),
			"tier1_only":  tier1Only,
			"days":        days,
			"total_found": len(events),
		},
		DurationMs: durationMs(started),
	}
}

func (e *ToolExecutor) context(ctx context.Context, args map[string]any, ident memory.MemoryIdentity, started time.Time) tools.ExecutionResult {
	provider, ok := e.provider.(workingMemoryContextProvider)
	if !ok {
		return stateError("working memory context is not available in this runtime", started)
	}
	patchSection, _ := args["patch_section"].(string)
	patchSection = strings.TrimSpace(patchSection)
	patchContent, hasPatchContent := args["patch_content"]
	patchAppend, hasPatchAppend := args["patch_append"]
	if patchSection == "" {
		if hasPatchContent || hasPatchAppend {
			return argError("patch_section is required when patch_content or patch_append is provided", started)
		}
		wm, err := provider.ReadWorkingMemory(ctx, ident)
		if err != nil {
			return providerError("context", err, started)
		}
		return tools.ExecutionResult{
			ResultJSON: map[string]any{
				"content":   wm.Content,
				"available": wm.Available,
				"source":    "working_memory",
			},
			DurationMs: durationMs(started),
		}
	}
	if !hasPatchContent && !hasPatchAppend {
		return argError("patch_section requires patch_content or patch_append", started)
	}
	patch := nowledge.WorkingMemoryPatch{}
	action := "replace"
	if hasPatchContent {
		value, ok := patchContent.(string)
		if !ok {
			return argError("patch_content must be a string", started)
		}
		patch.Content = &value
	}
	if hasPatchAppend {
		value, ok := patchAppend.(string)
		if !ok {
			return argError("patch_append must be a string", started)
		}
		patch.Append = &value
		action = "append"
	}
	wm, err := provider.PatchWorkingMemory(ctx, ident, patchSection, patch)
	if err != nil {
		return providerError("context", err, started)
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"status":          "ok",
			"action":          action,
			"patched_section": patchSection,
			"content":         wm.Content,
			"available":       wm.Available,
			"source":          "working_memory",
		},
		DurationMs: durationMs(started),
	}
}

func (e *ToolExecutor) status(ctx context.Context, ident memory.MemoryIdentity, started time.Time) tools.ExecutionResult {
	provider, ok := e.provider.(statusProvider)
	if !ok {
		return stateError("memory status is not available in this runtime", started)
	}
	status, err := provider.Status(ctx, ident)
	if err != nil {
		return providerError("status", err, started)
	}
	result := map[string]any{
		"provider":           "nowledge",
		"mode":               status.Mode,
		"base_url":           status.BaseURL,
		"api_key_configured": status.APIKeyConfigured,
		"healthy":            status.Healthy,
		"version":            status.Version,
	}
	if status.DatabaseConnected != nil {
		result["database_connected"] = *status.DatabaseConnected
	}
	if status.WorkingMemoryAvailable != nil {
		result["working_memory_available"] = *status.WorkingMemoryAvailable
	}
	if strings.TrimSpace(status.Error) != "" {
		result["error"] = strings.TrimSpace(status.Error)
	}
	return tools.ExecutionResult{ResultJSON: result, DurationMs: durationMs(started)}
}

func summarizeMemoryReadDesktop(title, content, depth string) string {
	title = strings.TrimSpace(title)
	content = strings.TrimSpace(content)
	if strings.EqualFold(depth, "full") {
		if title == "" {
			return content
		}
		if content == "" {
			return title
		}
		return title + "\n\n" + content
	}
	summary := compactReadSnippetDesktop(firstReadValueDesktop(title, content), 240)
	if title == "" || summary == title || content == "" {
		return summary
	}
	return title + "\n\n" + summary
}

func sliceReadLinesDesktop(text string, fromLine, lineCount int) (string, int, int, int) {
	allLines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	start := fromLine
	if start <= 0 {
		start = 1
	}
	total := len(allLines)
	if total == 1 && allLines[0] == "" {
		total = 0
	}
	if total == 0 {
		return "", start, start, 0
	}
	maxLines := lineCount
	if maxLines <= 0 {
		maxLines = total
	}
	startIdx := start - 1
	if startIdx >= total {
		return "", start, start, total
	}
	endIdx := startIdx + maxLines
	if endIdx > total {
		endIdx = total
	}
	selected := allLines[startIdx:endIdx]
	endLine := start + len(selected) - 1
	if len(selected) == 0 {
		endLine = start
	}
	return strings.Join(selected, "\n"), start, endLine, total
}

func compactReadSnippetDesktop(text string, maxRunes int) string {
	text = strings.TrimSpace(strings.Join(strings.Fields(text), " "))
	if text == "" || maxRunes <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "..."
}

func firstReadValueDesktop(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
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
	source, _ := args["source"].(string)
	limit := parseLimit(args, defaultSearchLimit)
	var (
		data map[string]any
		err  error
	)
	if strings.TrimSpace(source) != "" {
		filteredProvider, ok := e.provider.(threadSearchFilterProvider)
		if !ok {
			return stateError("thread source filtering is not available in this runtime", started)
		}
		data, err = filteredProvider.SearchThreadsFull(ctx, ident, strings.TrimSpace(query), limit, strings.TrimSpace(source))
	} else {
		data, err = provider.SearchThreads(ctx, ident, strings.TrimSpace(query), limit)
	}
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
		if e.db != nil && isNowledgeProvider(e.provider) {
			pipeline.EditSnapshotRefresh(e.provider, pipeline.NewDesktopMemorySnapshotStore(e.db), e.db, execCtx.RunID, execCtx.TraceID, ident, strings.TrimSpace(content))
			if e.impStore != nil {
				pipeline.BumpImpressionScore(ctx, e.impStore, ident, 5, e.configResolver, e.impRefresh)
			}
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
	if e.db != nil && shouldScheduleDesktopSnapshotRefresh(e.provider) {
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
	if e.db != nil && shouldScheduleDesktopSnapshotRefresh(e.provider) {
		pipeline.EditSnapshotRefresh(e.provider, pipeline.NewDesktopMemorySnapshotStore(e.db), e.db, execCtx.RunID, execCtx.TraceID, ident, strings.TrimSpace(content))
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{"status": "ok", "uri": strings.TrimSpace(uri)},
		DurationMs: durationMs(started),
	}
}

func isNowledgeProvider(provider memory.MemoryProvider) bool {
	_, ok := provider.(*nowledge.Client)
	return ok
}

func shouldScheduleDesktopSnapshotRefresh(provider memory.MemoryProvider) bool {
	if isNowledgeProvider(provider) {
		return true
	}
	_, local := provider.(memory.DesktopLocalMemoryWriteURI)
	return !local
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

func renderThreadReadContentDesktop(data map[string]any, depth string) string {
	title, _ := data["title"].(string)
	source, _ := data["source"].(string)
	messages, _ := data["messages"].([]map[string]any)
	if len(messages) == 0 {
		if rawMessages, ok := data["messages"].([]any); ok {
			for _, item := range rawMessages {
				if msg, ok := item.(map[string]any); ok {
					messages = append(messages, msg)
				}
			}
		}
	}
	var builder strings.Builder
	if strings.TrimSpace(title) != "" {
		builder.WriteString(strings.TrimSpace(title))
		builder.WriteString("\n")
	}
	if strings.TrimSpace(source) != "" {
		builder.WriteString("source: ")
		builder.WriteString(strings.TrimSpace(source))
		builder.WriteString("\n")
	}
	if builder.Len() > 0 {
		builder.WriteString("\n")
	}
	limit := len(messages)
	if strings.EqualFold(strings.TrimSpace(depth), "overview") && limit > 6 {
		limit = 6
	}
	for index := 0; index < limit; index++ {
		role, _ := messages[index]["role"].(string)
		content, _ := messages[index]["content"].(string)
		role = strings.TrimSpace(role)
		content = strings.TrimSpace(content)
		if role == "" && content == "" {
			continue
		}
		if role == "" {
			role = "message"
		}
		builder.WriteString(role)
		builder.WriteString(": ")
		builder.WriteString(content)
		builder.WriteString("\n")
	}
	if strings.EqualFold(strings.TrimSpace(depth), "overview") && len(messages) > limit {
		builder.WriteString("...")
	}
	return strings.TrimSpace(builder.String())
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

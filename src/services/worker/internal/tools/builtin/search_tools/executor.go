package searchtools

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	sharedtoolmeta "arkloop/services/shared/toolmeta"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

const (
	errorArgsInvalid = "tool.args_invalid"
	errorNoResults   = "tool.no_results"
)

// Executor resolves tool queries against the searchable pool and activates matches.
type Executor struct {
	activator        tools.ToolActivator
	specsFn          func() map[string]llm.ToolSpec
	activeSpecsFn    func() map[string]llm.ToolSpec // core tools already in request.Tools
	alreadyActivated map[string]struct{}
}

// NewExecutor creates a search_tools executor.
// specsFn is called lazily because searchable specs are set after executor creation.
// activeSpecsFn (optional) provides core/already-active specs so queries against them
// return schema info with already_active:true rather than "no results".
func NewExecutor(activator tools.ToolActivator, specsFn func() map[string]llm.ToolSpec, activeSpecsFn func() map[string]llm.ToolSpec) *Executor {
	return &Executor{
		activator:        activator,
		specsFn:          specsFn,
		activeSpecsFn:    activeSpecsFn,
		alreadyActivated: map[string]struct{}{},
	}
}

func (e *Executor) Execute(
	ctx context.Context,
	toolName string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	toolCallID string,
) tools.ExecutionResult {
	started := time.Now()

	rawQueries, ok := args["queries"]
	if !ok {
		return errorResult(started, errorArgsInvalid, "queries is required")
	}
	queriesSlice, ok := rawQueries.([]any)
	if !ok || len(queriesSlice) == 0 {
		return errorResult(started, errorArgsInvalid, "queries must be a non-empty array of strings")
	}

	queries := make([]string, 0, len(queriesSlice))
	for _, q := range queriesSlice {
		if s, ok := q.(string); ok && strings.TrimSpace(s) != "" {
			queries = append(queries, strings.TrimSpace(s))
		}
	}
	if len(queries) == 0 {
		return errorResult(started, errorArgsInvalid, "queries must contain at least one non-empty string")
	}

	searchable := e.specsFn()

	// Build combined pool: searchable tools + already-active core tools.
	// Core tools are matched for schema lookup but never re-activated.
	var activeSpecs map[string]llm.ToolSpec
	if e.activeSpecsFn != nil {
		activeSpecs = e.activeSpecsFn()
	}
	pool := make(map[string]llm.ToolSpec, len(searchable)+len(activeSpecs))
	for k, v := range activeSpecs {
		pool[k] = v
	}
	for k, v := range searchable {
		pool[k] = v
	}

	matched := matchTools(queries, pool)

	if len(matched) == 0 {
		return tools.ExecutionResult{
			ResultJSON: map[string]any{
				"matched": []any{},
				"message": "no tools matched the given queries",
			},
			DurationMs: durationMs(started),
		}
	}

	// Activate searchable tools not yet injected; core tools are already active.
	var newSpecs []llm.ToolSpec
	results := make([]map[string]any, 0, len(matched))
	for _, spec := range matched {
		entry := specToJSON(spec)
		_, isSearchable := searchable[spec.Name]
		if isSearchable {
			if _, done := e.alreadyActivated[spec.Name]; !done {
				e.alreadyActivated[spec.Name] = struct{}{}
				newSpecs = append(newSpecs, spec)
			}
		} else {
			entry["already_active"] = true
		}
		results = append(results, entry)
	}

	if len(newSpecs) > 0 {
		e.activator.Activate(newSpecs...)
	}

	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"matched": results,
			"count":   len(results),
		},
		DurationMs: durationMs(started),
	}
}

func matchTools(queries []string, pool map[string]llm.ToolSpec) []llm.ToolSpec {
	seen := map[string]struct{}{}
	var result []llm.ToolSpec

	for _, query := range queries {
		q := strings.ToLower(query)

		// wildcard: return all searchable tools
		if q == "*" || q == "all" {
			for name, spec := range pool {
				if _, dup := seen[name]; !dup {
					seen[name] = struct{}{}
					result = append(result, spec)
				}
			}
			continue
		}

		// priority 1: exact name match
		if spec, ok := pool[query]; ok {
			if _, dup := seen[spec.Name]; !dup {
				seen[spec.Name] = struct{}{}
				result = append(result, spec)
			}
			continue
		}

		// priority 2: name contains query, or query contains name
		for name, spec := range pool {
			if _, dup := seen[name]; dup {
				continue
			}
			nameLower := strings.ToLower(name)
			if strings.Contains(nameLower, q) || strings.Contains(q, nameLower) {
				seen[name] = struct{}{}
				result = append(result, spec)
				continue
			}

			// priority 3: ShortDesc or Label match (via toolmeta)
			if meta, ok := sharedtoolmeta.Lookup(name); ok {
				combined := strings.ToLower(meta.ShortDesc + " " + meta.Label)
				if strings.Contains(combined, q) {
					seen[name] = struct{}{}
					result = append(result, spec)
				}
			}
		}
	}
	return result
}

func specToJSON(spec llm.ToolSpec) map[string]any {
	out := map[string]any{
		"name":   spec.Name,
		"schema": spec.JSONSchema,
	}
	if spec.Description != nil {
		out["description"] = *spec.Description
	}
	return out
}

func errorResult(started time.Time, class, message string) tools.ExecutionResult {
	return tools.ExecutionResult{
		Error: &tools.ExecutionError{
			ErrorClass: class,
			Message:    message,
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

// BuildCatalogPrompt builds a compact tool catalog for the system prompt.
// Only includes tools in the searchable set (not already in core).
func BuildCatalogPrompt(searchable map[string]llm.ToolSpec) string {
	if len(searchable) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n<available_tools>\n")
	sb.WriteString("Use search_tools to get the full schema before calling any of these tools.\n")

	for name := range searchable {
		shortDesc := name
		if meta, ok := sharedtoolmeta.Lookup(name); ok && meta.ShortDesc != "" {
			shortDesc = meta.ShortDesc
		}
		sb.WriteString("- ")
		sb.WriteString(name)
		sb.WriteString(": ")
		sb.WriteString(shortDesc)
		sb.WriteString("\n")
	}
	sb.WriteString("</available_tools>")
	return sb.String()
}

// MarshalSearchableIndex returns a JSON-serializable index for debugging/logging.
func MarshalSearchableIndex(searchable map[string]llm.ToolSpec) string {
	index := make(map[string]string, len(searchable))
	for name := range searchable {
		if meta, ok := sharedtoolmeta.Lookup(name); ok {
			index[name] = meta.ShortDesc
		} else {
			index[name] = ""
		}
	}
	data, _ := json.Marshal(index)
	return string(data)
}

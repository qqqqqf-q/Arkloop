package pipeline

import (
	"context"
	"strings"
	"time"

	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

type HookRuntime struct {
	registry *HookRegistry
	applier  HookResultApplier
}

func NewHookRuntime(registry *HookRegistry, applier HookResultApplier) *HookRuntime {
	if registry == nil {
		registry = NewHookRegistry()
	}
	if applier == nil {
		applier = NewDefaultHookResultApplier()
	}
	return &HookRuntime{
		registry: registry,
		applier:  applier,
	}
}

func (r *HookRuntime) Registry() *HookRegistry {
	if r == nil {
		return nil
	}
	return r.registry
}

func (r *HookRuntime) ResultApplier() HookResultApplier {
	if r == nil {
		return nil
	}
	return r.applier
}

func (r *HookRuntime) BeforePromptAssemble(ctx context.Context, rc *RunContext) PromptFragments {
	if r == nil || r.registry == nil {
		return nil
	}
	var out PromptFragments
	for _, hook := range r.registry.beforePromptHooks() {
		start := time.Now()
		name := providerName(hook)
		traceHook(rc, HookBeforePromptAssemble, name, "invoked", 0, "", 0)
		fragments, err := hook.BeforePromptAssemble(ctx, rc)
		if err != nil {
			traceHook(rc, HookBeforePromptAssemble, name, "failed", 0, err.Error(), time.Since(start).Milliseconds())
			continue
		}
		traceHook(rc, HookBeforePromptAssemble, name, "completed", len(fragments), "", time.Since(start).Milliseconds())
		out = append(out, fragments...)
	}
	return sortPromptFragments(out)
}

func (r *HookRuntime) AfterPromptAssemble(ctx context.Context, rc *RunContext, assembledPrompt string) PromptFragments {
	if r == nil || r.registry == nil {
		return nil
	}
	var out PromptFragments
	for _, hook := range r.registry.afterPromptHooks() {
		start := time.Now()
		name := providerName(hook)
		traceHook(rc, HookAfterPromptAssemble, name, "invoked", 0, "", 0)
		fragments, err := hook.AfterPromptAssemble(ctx, rc, assembledPrompt)
		if err != nil {
			traceHook(rc, HookAfterPromptAssemble, name, "failed", 0, err.Error(), time.Since(start).Milliseconds())
			continue
		}
		traceHook(rc, HookAfterPromptAssemble, name, "completed", len(fragments), "", time.Since(start).Milliseconds())
		out = append(out, fragments...)
	}
	return sortPromptFragments(out)
}

func (r *HookRuntime) BeforeModelCall(ctx context.Context, rc *RunContext, request llm.Request) ModelCallHints {
	if r == nil || r.registry == nil {
		return nil
	}
	var out ModelCallHints
	for _, hook := range r.registry.beforeModelHooks() {
		start := time.Now()
		name := providerName(hook)
		traceHook(rc, HookBeforeModelCall, name, "invoked", 0, "", 0)
		hints, err := hook.BeforeModelCall(ctx, rc, request)
		if err != nil {
			traceHook(rc, HookBeforeModelCall, name, "failed", 0, err.Error(), time.Since(start).Milliseconds())
			continue
		}
		traceHook(rc, HookBeforeModelCall, name, "completed", len(hints), "", time.Since(start).Milliseconds())
		out = append(out, hints...)
	}
	return sortModelCallHints(out)
}

func (r *HookRuntime) AfterModelResponse(ctx context.Context, rc *RunContext, response ModelResponse) PostResponseActions {
	if r == nil || r.registry == nil {
		return nil
	}
	var out PostResponseActions
	for _, hook := range r.registry.afterModelHooks() {
		start := time.Now()
		name := providerName(hook)
		traceHook(rc, HookAfterModelResponse, name, "invoked", 0, "", 0)
		actions, err := hook.AfterModelResponse(ctx, rc, response)
		if err != nil {
			traceHook(rc, HookAfterModelResponse, name, "failed", 0, err.Error(), time.Since(start).Milliseconds())
			continue
		}
		traceHook(rc, HookAfterModelResponse, name, "completed", len(actions), "", time.Since(start).Milliseconds())
		out = append(out, actions...)
	}
	return sortPostResponseActions(out)
}

func (r *HookRuntime) AfterToolCall(ctx context.Context, rc *RunContext, toolCall llm.ToolCall, toolResult tools.ExecutionResult) PostToolActions {
	if r == nil || r.registry == nil {
		return nil
	}
	var out PostToolActions
	for _, hook := range r.registry.afterToolHooks() {
		start := time.Now()
		name := providerName(hook)
		traceHook(rc, HookAfterToolCall, name, "invoked", 0, "", 0)
		actions, err := hook.AfterToolCall(ctx, rc, toolCall, toolResult)
		if err != nil {
			traceHook(rc, HookAfterToolCall, name, "failed", 0, err.Error(), time.Since(start).Milliseconds())
			continue
		}
		traceHook(rc, HookAfterToolCall, name, "completed", len(actions), "", time.Since(start).Milliseconds())
		out = append(out, actions...)
	}
	return sortPostToolActions(out)
}

func (r *HookRuntime) BeforeCompact(ctx context.Context, rc *RunContext, input CompactInput) CompactHints {
	if r == nil || r.registry == nil {
		return nil
	}
	var out CompactHints
	for _, hook := range r.registry.beforeCompactHooks() {
		start := time.Now()
		name := providerName(hook)
		traceHook(rc, HookBeforeCompact, name, "invoked", 0, "", 0)
		hints, err := hook.BeforeCompact(ctx, rc, input)
		if err != nil {
			traceHook(rc, HookBeforeCompact, name, "failed", 0, err.Error(), time.Since(start).Milliseconds())
			continue
		}
		traceHook(rc, HookBeforeCompact, name, "completed", len(hints), "", time.Since(start).Milliseconds())
		out = append(out, hints...)
	}
	return sortCompactHints(out)
}

func (r *HookRuntime) AfterCompact(ctx context.Context, rc *RunContext, output CompactOutput) PostCompactActions {
	if r == nil || r.registry == nil {
		return nil
	}
	var out PostCompactActions
	for _, hook := range r.registry.afterCompactHooks() {
		start := time.Now()
		name := providerName(hook)
		traceHook(rc, HookAfterCompact, name, "invoked", 0, "", 0)
		actions, err := hook.AfterCompact(ctx, rc, output)
		if err != nil {
			traceHook(rc, HookAfterCompact, name, "failed", 0, err.Error(), time.Since(start).Milliseconds())
			continue
		}
		traceHook(rc, HookAfterCompact, name, "completed", len(actions), "", time.Since(start).Milliseconds())
		out = append(out, actions...)
	}
	return sortPostCompactActions(out)
}

func (r *HookRuntime) BeforeThreadPersist(ctx context.Context, rc *RunContext, delta ThreadDelta) ThreadPersistHints {
	if r == nil || r.registry == nil {
		return nil
	}
	var out ThreadPersistHints
	for _, hook := range r.registry.beforeThreadHooks() {
		start := time.Now()
		name := providerName(hook)
		traceHook(rc, HookBeforeThreadPersist, name, "invoked", 0, "", 0)
		hints, err := hook.BeforeThreadPersist(ctx, rc, delta)
		if err != nil {
			traceHook(rc, HookBeforeThreadPersist, name, "failed", 0, err.Error(), time.Since(start).Milliseconds())
			continue
		}
		traceHook(rc, HookBeforeThreadPersist, name, "completed", len(hints), "", time.Since(start).Milliseconds())
		out = append(out, hints...)
	}
	return sortThreadPersistHints(out)
}

func (r *HookRuntime) ExecuteThreadPersist(ctx context.Context, rc *RunContext, delta ThreadDelta, hints ThreadPersistHints) ThreadPersistResult {
	if r == nil || r.registry == nil || r.registry.threadProvider == nil {
		return ThreadPersistResult{}
	}
	provider := r.registry.threadProvider
	start := time.Now()
	name := providerName(provider)
	traceHook(rc, HookBeforeThreadPersist, name, "invoked", 0, "", 0)
	result := provider.PersistThread(ctx, rc, delta, hints)
	if strings.TrimSpace(result.Provider) == "" {
		result.Provider = name
	}
	if result.Err != nil {
		traceHook(rc, HookBeforeThreadPersist, name, "failed", 0, result.Err.Error(), time.Since(start).Milliseconds())
		return result
	}
	traceHook(rc, HookBeforeThreadPersist, name, "completed", boolCount(result.Handled), "", time.Since(start).Milliseconds())
	return result
}

func (r *HookRuntime) AfterThreadPersist(ctx context.Context, rc *RunContext, delta ThreadDelta, result ThreadPersistResult) PersistObservers {
	if r == nil || r.registry == nil {
		return nil
	}
	var out PersistObservers
	for _, hook := range r.registry.afterThreadHooks() {
		start := time.Now()
		name := providerName(hook)
		traceHook(rc, HookAfterThreadPersist, name, "invoked", 0, "", 0)
		observers, err := hook.AfterThreadPersist(ctx, rc, delta, result)
		if err != nil {
			traceHook(rc, HookAfterThreadPersist, name, "failed", 0, err.Error(), time.Since(start).Milliseconds())
			continue
		}
		traceHook(rc, HookAfterThreadPersist, name, "completed", len(observers), "", time.Since(start).Milliseconds())
		out = append(out, observers...)
	}
	return sortPersistObservers(out)
}

func boolCount(v bool) int {
	if v {
		return 1
	}
	return 0
}

func traceHook(rc *RunContext, hook HookName, provider, status string, resultCount int, err string, durationMs int64) {
	fields := map[string]any{
		"hook_name":    string(hook),
		"provider":     strings.TrimSpace(provider),
		"duration_ms":  durationMs,
		"status":       strings.TrimSpace(status),
		"result_count": resultCount,
	}
	if strings.TrimSpace(err) != "" {
		fields["error"] = strings.TrimSpace(err)
	}
	eventName := "runtime_hook." + strings.TrimSpace(status)
	emitTraceEvent(rc, "runtime_hook", eventName, fields)
}

func sortModelCallHints(hints ModelCallHints) ModelCallHints {
	filtered := make(ModelCallHints, 0, len(hints))
	for _, hint := range hints {
		if strings.TrimSpace(hint.Key) == "" {
			continue
		}
		filtered = append(filtered, hint)
	}
	sortByPriority(filtered, func(i int) int { return filtered[i].Priority })
	return filtered
}

func sortPostResponseActions(actions PostResponseActions) PostResponseActions {
	filtered := make(PostResponseActions, 0, len(actions))
	for _, action := range actions {
		if strings.TrimSpace(action.Key) == "" {
			continue
		}
		filtered = append(filtered, action)
	}
	sortByPriority(filtered, func(i int) int { return filtered[i].Priority })
	return filtered
}

func sortPostToolActions(actions PostToolActions) PostToolActions {
	filtered := make(PostToolActions, 0, len(actions))
	for _, action := range actions {
		if strings.TrimSpace(action.Key) == "" {
			continue
		}
		filtered = append(filtered, action)
	}
	sortByPriority(filtered, func(i int) int { return filtered[i].Priority })
	return filtered
}

func sortPostCompactActions(actions PostCompactActions) PostCompactActions {
	filtered := make(PostCompactActions, 0, len(actions))
	for _, action := range actions {
		if strings.TrimSpace(action.Key) == "" {
			continue
		}
		filtered = append(filtered, action)
	}
	sortByPriority(filtered, func(i int) int { return filtered[i].Priority })
	return filtered
}

func sortThreadPersistHints(hints ThreadPersistHints) ThreadPersistHints {
	filtered := make(ThreadPersistHints, 0, len(hints))
	for _, hint := range hints {
		if strings.TrimSpace(hint.Key) == "" {
			continue
		}
		filtered = append(filtered, hint)
	}
	sortByPriority(filtered, func(i int) int { return filtered[i].Priority })
	return filtered
}

func sortPersistObservers(observers PersistObservers) PersistObservers {
	filtered := make(PersistObservers, 0, len(observers))
	for _, observer := range observers {
		if strings.TrimSpace(observer.Key) == "" {
			continue
		}
		filtered = append(filtered, observer)
	}
	sortByPriority(filtered, func(i int) int { return filtered[i].Priority })
	return filtered
}

func sortByPriority[T any](items []T, priority func(index int) int) {
	if len(items) < 2 {
		return
	}
	for i := 1; i < len(items); i++ {
		j := i
		for j > 0 && priority(j) < priority(j-1) {
			items[j], items[j-1] = items[j-1], items[j]
			j--
		}
	}
}

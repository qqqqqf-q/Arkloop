package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"arkloop/services/shared/pluginhook"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

const defaultPluginHookTimeout = 5 * time.Second

var ErrPluginModelDenied = errors.New("plugin before_model_call denied")

type DefaultPluginHookRunner struct{}

type PluginBeforeToolResult struct {
	Call   llm.ToolCall
	Result *tools.ExecutionResult
}

func NewDefaultPluginHookRunner() *DefaultPluginHookRunner {
	return &DefaultPluginHookRunner{}
}

func (DefaultPluginHookRunner) RunPluginHook(ctx context.Context, inv PluginHookInvocation) (PluginHookResult, error) {
	config := inv.Hook.HookConfig
	if strings.TrimSpace(config.PluginID) == "" {
		config.PluginID = inv.Hook.PluginID
	}
	if config.Event == "" {
		config.Event = pluginhook.HookEvent(toSharedPluginHookEvent(inv.Event))
	}
	if config.TimeoutMS <= 0 && inv.Hook.Timeout > 0 {
		config.TimeoutMS = int(inv.Hook.Timeout / time.Millisecond)
	}
	input := pluginhook.HookInput{
		Event:    config.Event,
		PluginID: config.PluginID,
		RunID:    inv.RunID.String(),
		Payload:  pluginHookPayload(inv),
	}
	var output pluginhook.HookOutput
	var err error
	switch config.Type {
	case pluginhook.HookTypeCommand:
		output, err = pluginhook.NewCommandHookRunner(config).Run(ctx, input)
	case pluginhook.HookTypeHTTP:
		output, err = pluginhook.NewHTTPHookRunner(config, nil).Run(ctx, input)
	default:
		return PluginHookResult{}, fmt.Errorf("plugin hook type %q is unsupported", config.Type)
	}
	if err != nil {
		return PluginHookResult{}, err
	}
	return pluginHookResultFromOutput(output), nil
}

func RunPluginSessionStart(ctx context.Context, rc *RunContext) {
	runPluginObserveHooks(ctx, rc, PluginHookSessionStart, PluginHookInvocation{})
}

func RunPluginSessionEnd(ctx context.Context, rc *RunContext, sessionErr error) {
	inv := PluginHookInvocation{}
	if sessionErr != nil {
		inv.SessionError = sessionErr.Error()
	}
	runPluginObserveHooks(ctx, rc, PluginHookSessionEnd, inv)
}

func RunPluginBeforeToolUse(ctx context.Context, rc *RunContext, call llm.ToolCall) PluginBeforeToolResult {
	out := PluginBeforeToolResult{Call: call}
	if rc == nil || rc.PluginHookRunner == nil {
		return out
	}
	for _, hook := range pluginHooksForEvent(rc.PluginHooks, PluginHookBeforeToolUse) {
		result, ok := runPluginHook(ctx, rc, hook, PluginHookInvocation{ToolCall: out.Call})
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(result.Decision)) {
		case "deny", "denied":
			denied := pluginDeniedToolResult(out.Call, result)
			out.Result = &denied
			return out
		case "modify", "modified":
			if result.ModifiedArgs != nil {
				out.Call.ArgumentsJSON = copyPluginMap(result.ModifiedArgs)
			}
		}
	}
	return out
}

func RunPluginAfterToolUse(ctx context.Context, rc *RunContext, call llm.ToolCall, result tools.ExecutionResult) {
	runPluginObserveHooks(ctx, rc, PluginHookAfterToolUse, PluginHookInvocation{ToolCall: call, ToolResult: result})
}

func RunPluginBeforeModelCall(ctx context.Context, rc *RunContext, request llm.Request) (llm.Request, error) {
	if rc == nil || rc.PluginHookRunner == nil {
		return request, nil
	}
	out := request
	for _, hook := range pluginHooksForEvent(rc.PluginHooks, PluginHookBeforeModelCall) {
		result, ok := runPluginHook(ctx, rc, hook, PluginHookInvocation{ModelRequest: out})
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(result.Decision)) {
		case "deny", "denied":
			message := strings.TrimSpace(result.Message)
			if message == "" {
				message = "plugin before_model_call denied the request"
			}
			return out, fmt.Errorf("%w: %s", ErrPluginModelDenied, message)
		}
		out = appendPluginModelSegments(rc, out, result.InjectSegments)
	}
	return out, nil
}

func RunPluginAfterModelResponse(ctx context.Context, rc *RunContext, response ModelResponse) {
	runPluginObserveHooks(ctx, rc, PluginHookAfterModelResponse, PluginHookInvocation{ModelResponse: response})
}

func runPluginObserveHooks(ctx context.Context, rc *RunContext, event string, inv PluginHookInvocation) {
	if rc == nil || rc.PluginHookRunner == nil {
		return
	}
	for _, hook := range pluginHooksForEvent(rc.PluginHooks, event) {
		_, _ = runPluginHook(ctx, rc, hook, inv)
	}
}

func runPluginHook(ctx context.Context, rc *RunContext, hook PluginHookConfig, inv PluginHookInvocation) (PluginHookResult, bool) {
	timeout := hook.Timeout
	if timeout <= 0 {
		timeout = defaultPluginHookTimeout
	}
	hookCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	inv.AccountID = rc.Run.AccountID
	inv.RunID = rc.Run.ID
	inv.ThreadID = rc.Run.ThreadID
	inv.ProfileRef = rc.ProfileRef
	inv.WorkspaceRef = rc.WorkspaceRef
	inv.Hook = hook
	inv.Event = hook.Event

	start := time.Now()
	tracePluginHook(rc, hook, "invoked", 0, "", 0)
	result, err := rc.PluginHookRunner.RunPluginHook(hookCtx, inv)
	if err != nil {
		tracePluginHook(rc, hook, "failed", 0, err.Error(), time.Since(start).Milliseconds())
		return PluginHookResult{}, false
	}
	tracePluginHook(rc, hook, "completed", len(result.InjectSegments), "", time.Since(start).Milliseconds())
	return result, true
}

func pluginHooksForEvent(hooks []PluginHookConfig, event string) []PluginHookConfig {
	event = normalizePluginHookEvent(event)
	if event == "" || len(hooks) == 0 {
		return nil
	}
	out := make([]PluginHookConfig, 0, len(hooks))
	for _, hook := range hooks {
		if normalizePluginHookEvent(hook.Event) == event {
			out = append(out, hook)
		}
	}
	return out
}

func pluginDeniedToolResult(call llm.ToolCall, result PluginHookResult) tools.ExecutionResult {
	message := strings.TrimSpace(result.Message)
	if message == "" {
		message = "tool use denied by plugin hook"
	}
	errorClass := strings.TrimSpace(result.ErrorClass)
	if errorClass == "" {
		errorClass = tools.PolicyDeniedCode
	}
	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"denied": true,
			"reason": message,
		},
		Error: &tools.ExecutionError{
			ErrorClass: errorClass,
			Message:    message,
			Details: map[string]any{
				"tool_name": call.ToolName,
			},
		},
	}
}

func appendPluginModelSegments(rc *RunContext, request llm.Request, segments []PromptSegment) llm.Request {
	for _, segment := range segments {
		normalized, ok := normalizePromptSegment(segment)
		if !ok || normalized.Role != "system" {
			continue
		}
		if strings.TrimSpace(normalized.Name) == "" {
			continue
		}
		if !strings.HasPrefix(normalized.Name, "plugin.hook.") {
			normalized.Name = "plugin.hook." + sanitizePromptSegmentName(normalized.Name)
		}
		request.Messages = appendSystemMessageSegment(request.Messages, normalized.Text)
	}
	return request
}

func appendSystemMessageSegment(messages []llm.Message, text string) []llm.Message {
	text = strings.TrimSpace(text)
	if text == "" {
		return messages
	}
	out := append([]llm.Message(nil), messages...)
	insertAt := 0
	for insertAt < len(out) && out[insertAt].Role == "system" {
		insertAt++
	}
	msg := llm.Message{Role: "system", Content: []llm.TextPart{{Text: text}}}
	out = append(out, llm.Message{})
	copy(out[insertAt+1:], out[insertAt:])
	out[insertAt] = msg
	return out
}

func normalizePluginHookEvent(event string) string {
	event = strings.TrimSpace(event)
	if event == "" {
		return ""
	}
	var out strings.Builder
	for i, r := range event {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				out.WriteByte('_')
			}
			out.WriteRune(r + ('a' - 'A'))
			continue
		}
		switch r {
		case '-', '.', ' ':
			out.WriteByte('_')
		default:
			out.WriteRune(r)
		}
	}
	normalized := strings.ToLower(strings.Trim(out.String(), "_"))
	switch normalized {
	case "before_run":
		return PluginHookSessionStart
	case "after_run":
		return PluginHookSessionEnd
	case "before_model":
		return PluginHookBeforeModelCall
	case "after_model":
		return PluginHookAfterModelResponse
	case "before_tool":
		return PluginHookBeforeToolUse
	case "after_tool":
		return PluginHookAfterToolUse
	default:
		return normalized
	}
}

func tracePluginHook(rc *RunContext, hook PluginHookConfig, status string, resultCount int, err string, durationMs int64) {
	fields := map[string]any{
		"plugin_id":   strings.TrimSpace(hook.PluginID),
		"hook_id":     strings.TrimSpace(hook.HookID),
		"hook_event":  normalizePluginHookEvent(hook.Event),
		"duration_ms": durationMs,
		"status":      strings.TrimSpace(status),
	}
	if resultCount > 0 {
		fields["result_count"] = resultCount
	}
	if strings.TrimSpace(err) != "" {
		fields["error"] = strings.TrimSpace(err)
	}
	emitTraceEvent(rc, "plugin_hook", "plugin_hook."+strings.TrimSpace(status), fields)
}

func copyPluginMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}

func toSharedPluginHookEvent(event string) string {
	switch normalizePluginHookEvent(event) {
	case PluginHookSessionStart:
		return string(pluginhook.EventBeforeRun)
	case PluginHookSessionEnd:
		return string(pluginhook.EventAfterRun)
	case PluginHookBeforeModelCall:
		return string(pluginhook.EventBeforeModel)
	case PluginHookAfterModelResponse:
		return string(pluginhook.EventAfterModel)
	case PluginHookBeforeToolUse:
		return string(pluginhook.EventBeforeTool)
	case PluginHookAfterToolUse:
		return string(pluginhook.EventAfterTool)
	default:
		return event
	}
}

func pluginHookPayload(inv PluginHookInvocation) map[string]any {
	payload := map[string]any{
		"account_id": inv.AccountID.String(),
	}
	switch normalizePluginHookEvent(inv.Event) {
	case PluginHookBeforeToolUse:
		payload["tool_name"] = inv.ToolCall.ToolName
		payload["args"] = inv.ToolCall.ArgumentsJSON
		payload["tool_call_id"] = inv.ToolCall.ToolCallID
	case PluginHookAfterToolUse:
		payload["tool_name"] = inv.ToolCall.ToolName
		payload["args"] = inv.ToolCall.ArgumentsJSON
		payload["tool_call_id"] = inv.ToolCall.ToolCallID
		payload["result_summary"] = summarizePluginToolResult(inv.ToolResult)
	case PluginHookBeforeModelCall:
		payload["model"] = inv.ModelRequest.Model
		payload["message_count"] = len(inv.ModelRequest.Messages)
	case PluginHookAfterModelResponse:
		payload["model"] = inv.ModelResponse.Model
		payload["usage"] = pluginModelUsage(inv.ModelResponse)
	case PluginHookSessionStart:
		payload["profile_ref"] = inv.ProfileRef
	case PluginHookSessionEnd:
		payload["exit_reason"] = firstNonEmptyPluginString(inv.SessionError, "completed")
	}
	return payload
}

func summarizePluginToolResult(result tools.ExecutionResult) map[string]any {
	out := map[string]any{}
	if result.ResultJSON != nil {
		out["result"] = result.ResultJSON
	}
	if result.Error != nil {
		out["error"] = result.Error.ToJSON()
	}
	return out
}

func pluginModelUsage(response ModelResponse) map[string]any {
	out := copyPluginMap(response.Completed)
	if out == nil {
		out = map[string]any{}
	}
	out["terminal"] = response.Terminal
	out["cancelled"] = response.Cancelled
	return out
}

func firstNonEmptyPluginString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func pluginHookResultFromOutput(output pluginhook.HookOutput) PluginHookResult {
	result := PluginHookResult{
		Decision: string(output.Action),
		Message:  firstNonEmptyPluginString(output.Reason, output.Message),
	}
	if strings.TrimSpace(output.Error) != "" && result.Message == "" {
		result.Message = strings.TrimSpace(output.Error)
	}
	rawModified := output.ModifiedArgs
	if rawModified == nil {
		rawModified = output.Modified
	}
	if rawModified != nil && len(*rawModified) > 0 {
		var modifiedArgs map[string]any
		if err := json.Unmarshal(*rawModified, &modifiedArgs); err == nil {
			result.ModifiedArgs = modifiedArgs
		}
	}
	for idx, segment := range output.InjectSegments {
		if strings.TrimSpace(segment.Content) == "" {
			continue
		}
		name := strings.TrimSpace(segment.Name)
		if name == "" {
			name = fmt.Sprintf("plugin.hook.inject.%03d", idx+1)
		}
		role := strings.TrimSpace(segment.Role)
		if role == "" {
			role = "system"
		}
		result.InjectSegments = append(result.InjectSegments, PromptSegment{
			Name:      name,
			Target:    PromptTargetSystemPrefix,
			Role:      role,
			Text:      segment.Content,
			Stability: PromptStabilitySessionPrefix,
		})
	}
	return result
}

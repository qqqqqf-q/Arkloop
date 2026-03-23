package spawnagent

import (
	"context"
	"fmt"
	"strings"
	"time"

	sharedconfig "arkloop/services/shared/config"
	sharedent "arkloop/services/shared/entitlement"
	"arkloop/services/shared/skillstore"
	sharedtoolmeta "arkloop/services/shared/toolmeta"
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/subagentctl"
	"arkloop/services/worker/internal/tools"

	"github.com/google/uuid"
)

const (
	errorArgsInvalid    = "tool.args_invalid"
	errorControlFailed  = "tool.sub_agent_failed"
	errorNotInitialized = "tool.not_initialized"
	errorTimeout        = "tool.timeout"
)

// BuiltinNames 返回所有 spawn_agent 系列内置工具名，供上游过滤冲突用。
var BuiltinNames = func() map[string]struct{} {
	names := []string{
		"spawn_agent", "send_input", "wait_agent",
		"resume_agent", "close_agent", "interrupt_agent",
	}
	m := make(map[string]struct{}, len(names))
	for _, n := range names {
		m[n] = struct{}{}
	}
	return m
}()

var AgentSpec = tools.AgentToolSpec{
	Name:        "spawn_agent",
	Version:     "2",
	Description: "spawn a sub-agent and return a handle for later control",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: true,
}

var SendInputSpec = tools.AgentToolSpec{
	Name:        "send_input",
	Version:     "1",
	Description: "send a follow-up input to a sub-agent",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: true,
}

var WaitAgentSpec = tools.AgentToolSpec{
	Name:        "wait_agent",
	Version:     "1",
	Description: "wait for a sub-agent to reach a resolved state",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: false,
}

var ResumeAgentSpec = tools.AgentToolSpec{
	Name:        "resume_agent",
	Version:     "1",
	Description: "resume a resumable sub-agent",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: true,
}

var CloseAgentSpec = tools.AgentToolSpec{
	Name:        "close_agent",
	Version:     "1",
	Description: "close a sub-agent handle",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: true,
}

var InterruptAgentSpec = tools.AgentToolSpec{
	Name:        "interrupt_agent",
	Version:     "1",
	Description: "interrupt an active sub-agent run",
	RiskLevel:   tools.RiskLevelMedium,
	SideEffects: true,
}

var LlmSpec = llm.ToolSpec{
	Name:        AgentSpec.Name,
	Description: stringPtr(sharedtoolmeta.Must(AgentSpec.Name).LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"persona_id": map[string]any{
				"type":        "string",
				"description": "ID of a registered persona in this project. Must match an available persona exactly.",
			},
			"role": map[string]any{
				"type":        "string",
				"description": "Optional role description for the sub-agent, visible in status snapshots.",
			},
			"nickname": map[string]any{
				"type":        "string",
				"description": "Optional display name for the sub-agent shown in the UI.",
			},
			"context_mode": map[string]any{
				"type":        "string",
				"enum":        []string{"isolated", "fork_recent", "fork_thread", "fork_selected", "shared_workspace_only"},
				"description": "How much parent context the sub-agent inherits. Use 'isolated' for independent tasks.",
			},
			"inherit": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"messages":     map[string]any{"type": "boolean"},
					"attachments":  map[string]any{"type": "boolean"},
					"workspace":    map[string]any{"type": "boolean"},
					"skills":       map[string]any{"type": "boolean"},
					"runtime":      map[string]any{"type": "boolean"},
					"memory_scope": map[string]any{"type": "string", "enum": []string{"same_user"}},
					"message_ids": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": "string"},
					},
				},
				"additionalProperties": false,
			},
			"input": map[string]any{
				"type":        "string",
				"description": "The task or message to send to the sub-agent.",
			},
			"profile": map[string]any{
				"type":        "string",
				"enum":        []string{"explore", "task", "strong"},
				"description": "Model capability tier for the sub-agent. explore=fast/cheap, task=balanced, strong=best reasoning.",
			},
		},
		"required":             []string{"persona_id", "context_mode", "input"},
		"additionalProperties": false,
	},
}

// LlmSpecWithPersonas returns a copy of LlmSpec with persona_id description
// enriched to list the available persona IDs.
func LlmSpecWithPersonas(personaKeys []string) llm.ToolSpec {
	spec := LlmSpec
	schema := copyJSONSchema(spec.JSONSchema)
	if props, ok := schema["properties"].(map[string]any); ok {
		if pid, ok := props["persona_id"].(map[string]any); ok {
			enriched := make(map[string]any, len(pid)+1)
			for k, v := range pid {
				enriched[k] = v
			}
			if len(personaKeys) > 0 {
				enriched["description"] = fmt.Sprintf(
					"ID of a registered persona. Available: %s",
					strings.Join(personaKeys, ", "),
				)
			}
			props["persona_id"] = enriched
		}
	}
	spec.JSONSchema = schema
	return spec
}

func copyJSONSchema(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		if m, ok := v.(map[string]any); ok {
			dst[k] = copyJSONSchema(m)
		} else {
			dst[k] = v
		}
	}
	return dst
}

var SendInputLlmSpec = llm.ToolSpec{
	Name:        SendInputSpec.Name,
	Description: stringPtr(sharedtoolmeta.Must(SendInputSpec.Name).LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"sub_agent_id": map[string]any{"type": "string"},
			"input":        map[string]any{"type": "string"},
			"interrupt":    map[string]any{"type": "boolean"},
		},
		"required":             []string{"sub_agent_id", "input"},
		"additionalProperties": false,
	},
}

var WaitAgentLlmSpec = llm.ToolSpec{
	Name:        WaitAgentSpec.Name,
	Description: stringPtr(sharedtoolmeta.Must(WaitAgentSpec.Name).LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ids": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"minItems":    1,
				"description": "One or more sub_agent_id values. Returns the first to reach a terminal state.",
			},
			"timeout_seconds": map[string]any{"type": "integer", "minimum": 1},
		},
		"required":             []string{"ids"},
		"additionalProperties": false,
	},
}

var ResumeAgentLlmSpec = llm.ToolSpec{
	Name:        ResumeAgentSpec.Name,
	Description: stringPtr(sharedtoolmeta.Must(ResumeAgentSpec.Name).LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"sub_agent_id": map[string]any{"type": "string"},
		},
		"required":             []string{"sub_agent_id"},
		"additionalProperties": false,
	},
}

var CloseAgentLlmSpec = llm.ToolSpec{
	Name:        CloseAgentSpec.Name,
	Description: stringPtr(sharedtoolmeta.Must(CloseAgentSpec.Name).LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"sub_agent_id": map[string]any{"type": "string"},
		},
		"required":             []string{"sub_agent_id"},
		"additionalProperties": false,
	},
}

var InterruptAgentLlmSpec = llm.ToolSpec{
	Name:        InterruptAgentSpec.Name,
	Description: stringPtr(sharedtoolmeta.Must(InterruptAgentSpec.Name).LLMDescription),
	JSONSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"sub_agent_id": map[string]any{"type": "string"},
			"reason":       map[string]any{"type": "string"},
		},
		"required":             []string{"sub_agent_id"},
		"additionalProperties": false,
	},
}

type ToolExecutor struct {
	Control             subagentctl.Control
	PersonaKeys         []string // 可用 persona ID 列表，spawn 前快速校验
	EntitlementResolver *sharedent.Resolver
	AccountID           uuid.UUID
}

func (e *ToolExecutor) Execute(
	ctx context.Context,
	toolName string,
	args map[string]any,
	execCtx tools.ExecutionContext,
	_ string,
) tools.ExecutionResult {
	started := time.Now()
	if e.Control == nil {
		return errorResult(errorNotInitialized, fmt.Sprintf("%s not available", toolName), started)
	}

	var (
		snapshot subagentctl.StatusSnapshot
		err      error
	)
	switch strings.TrimSpace(toolName) {
	case AgentSpec.Name:
		var req subagentctl.SpawnRequest
		req, err = parseSpawnArgs(args, execCtx)
		if err == nil {
			err = e.validatePersonaID(req.PersonaID)
		}
		if err == nil {
			e.resolveProfile(ctx, &req)
			snapshot, err = e.Control.Spawn(ctx, req)
		}
	case SendInputSpec.Name:
		var req subagentctl.SendInputRequest
		req, err = parseSendInputArgs(args)
		if err == nil {
			snapshot, err = e.Control.SendInput(ctx, req)
		}
	case WaitAgentSpec.Name:
		var req subagentctl.WaitRequest
		req, err = parseWaitArgs(args)
		if err == nil {
			snapshot, err = e.Control.Wait(ctx, req)
		}
	case ResumeAgentSpec.Name:
		var subAgentID uuid.UUID
		subAgentID, err = parseSubAgentIDArg(args)
		if err == nil {
			snapshot, err = e.Control.Resume(ctx, subagentctl.ResumeRequest{SubAgentID: subAgentID})
		}
	case CloseAgentSpec.Name:
		var subAgentID uuid.UUID
		subAgentID, err = parseSubAgentIDArg(args)
		if err == nil {
			snapshot, err = e.Control.Close(ctx, subagentctl.CloseRequest{SubAgentID: subAgentID})
		}
	case InterruptAgentSpec.Name:
		var req subagentctl.InterruptRequest
		req, err = parseInterruptArgs(args)
		if err == nil {
			snapshot, err = e.Control.Interrupt(ctx, req)
		}
	default:
		err = fmt.Errorf("unknown tool: %s", toolName)
	}
	if err != nil {
		if execErr, ok := err.(*tools.ExecutionError); ok {
			return tools.ExecutionResult{Error: execErr, DurationMs: durationMs(started)}
		}
		// wait_agent 超时但有最新快照时，返回快照 + timeout 标记
		if isTimeoutError(err) && snapshot.SubAgentID != uuid.Nil {
			result := snapshotJSON(snapshot)
			result["timeout"] = true
			result["timeout_ms"] = durationMs(started)
			return tools.ExecutionResult{ResultJSON: result, DurationMs: durationMs(started)}
		}
		if isTimeoutError(err) {
			return errorResult(errorTimeout, err.Error(), started)
		}
		return errorResult(errorControlFailed, err.Error(), started)
	}
	return tools.ExecutionResult{ResultJSON: snapshotJSON(snapshot), DurationMs: durationMs(started)}
}

// validatePersonaID 校验 persona_id 是否在预加载列表中
func (e *ToolExecutor) validatePersonaID(id string) error {
	if len(e.PersonaKeys) == 0 {
		return nil
	}
	for _, k := range e.PersonaKeys {
		if k == id {
			return nil
		}
	}
	return &tools.ExecutionError{
		ErrorClass: errorControlFailed,
		Message:    fmt.Sprintf("persona not found: %s, available: %v", id, e.PersonaKeys),
	}
}

// resolveProfile resolves profile to provider^model and writes it into req.ParentContext.Model.
func (e *ToolExecutor) resolveProfile(ctx context.Context, req *subagentctl.SpawnRequest) {
	if req.Profile == "" || e.EntitlementResolver == nil {
		return
	}
	key := "spawn.profile." + req.Profile
	val, err := e.EntitlementResolver.Resolve(ctx, e.AccountID, key)
	if err != nil || strings.TrimSpace(val) == "" {
		return
	}
	applyResolvedProfile(req, val)
}

func applyResolvedProfile(req *subagentctl.SpawnRequest, value string) bool {
	if req == nil {
		return false
	}
	mapping, err := sharedconfig.ParseProfileValue(value)
	if err != nil {
		return false
	}
	// 显式 profile override 应按 selector 重新选路，不再绑定父 run 的 route_id。
	req.ParentContext.RouteID = ""
	req.ParentContext.Model = mapping.Provider + "^" + mapping.Model
	return true
}

func parseSpawnArgs(args map[string]any, execCtx tools.ExecutionContext) (subagentctl.SpawnRequest, error) {
	for key := range args {
		switch key {
		case "persona_id", "role", "nickname", "context_mode", "inherit", "input", "profile":
		default:
			return subagentctl.SpawnRequest{}, argsError("unknown parameter: " + key)
		}
	}
	personaID, ok := args["persona_id"].(string)
	if !ok || strings.TrimSpace(personaID) == "" {
		return subagentctl.SpawnRequest{}, argsError("persona_id must be a non-empty string")
	}
	contextMode, ok := args["context_mode"].(string)
	if !ok || strings.TrimSpace(contextMode) == "" {
		return subagentctl.SpawnRequest{}, argsError("context_mode must be a non-empty string")
	}
	input, ok := args["input"].(string)
	if !ok || strings.TrimSpace(input) == "" {
		return subagentctl.SpawnRequest{}, argsError("input must be a non-empty string")
	}
	req := subagentctl.SpawnRequest{
		PersonaID:   strings.TrimSpace(personaID),
		ContextMode: strings.TrimSpace(contextMode),
		Input:       strings.TrimSpace(input),
		ParentContext: subagentctl.SpawnParentContext{
			ToolAllowlist: append([]string(nil), execCtx.ToolAllowlist...),
			ToolDenylist:  append([]string(nil), execCtx.ToolDenylist...),
			RouteID:       strings.TrimSpace(execCtx.RouteID),
			Model:         strings.TrimSpace(execCtx.Model),
			ProfileRef:    strings.TrimSpace(execCtx.ProfileRef),
			WorkspaceRef:  strings.TrimSpace(execCtx.WorkspaceRef),
			EnabledSkills: append([]skillstore.ResolvedSkill(nil), execCtx.EnabledSkills...),
			MemoryScope:   firstNonEmpty(strings.TrimSpace(execCtx.MemoryScope), subagentctl.MemoryScopeSameUser),
		},
	}
	if raw, ok := args["role"]; ok {
		value, ok := raw.(string)
		if !ok {
			return subagentctl.SpawnRequest{}, argsError("role must be a string")
		}
		cleaned := strings.TrimSpace(value)
		if cleaned != "" {
			req.Role = &cleaned
		}
	}
	if raw, ok := args["nickname"]; ok {
		value, ok := raw.(string)
		if !ok {
			return subagentctl.SpawnRequest{}, argsError("nickname must be a string")
		}
		cleaned := strings.TrimSpace(value)
		if cleaned != "" {
			req.Nickname = &cleaned
		}
	}
	if raw, ok := args["inherit"]; ok {
		parsed, err := parseSpawnInherit(raw)
		if err != nil {
			return subagentctl.SpawnRequest{}, err
		}
		req.Inherit = parsed
	}
	if raw, ok := args["profile"]; ok {
		value, ok := raw.(string)
		if !ok {
			return subagentctl.SpawnRequest{}, argsError("profile must be a string")
		}
		cleaned := strings.TrimSpace(strings.ToLower(value))
		if cleaned != "" {
			switch cleaned {
			case "explore", "task", "strong":
				req.Profile = cleaned
			default:
				return subagentctl.SpawnRequest{}, argsError("profile must be one of: explore, task, strong")
			}
		}
	}
	return req, nil
}

func parseSendInputArgs(args map[string]any) (subagentctl.SendInputRequest, error) {
	subAgentID, err := parseSubAgentIDArg(args)
	if err != nil {
		return subagentctl.SendInputRequest{}, err
	}
	input, ok := args["input"].(string)
	if !ok || strings.TrimSpace(input) == "" {
		return subagentctl.SendInputRequest{}, argsError("input must be a non-empty string")
	}
	interrupt := false
	if raw, ok := args["interrupt"]; ok {
		flag, ok := raw.(bool)
		if !ok {
			return subagentctl.SendInputRequest{}, argsError("interrupt must be a boolean")
		}
		interrupt = flag
	}
	return subagentctl.SendInputRequest{SubAgentID: subAgentID, Input: strings.TrimSpace(input), Interrupt: interrupt}, nil
}

func parseWaitArgs(args map[string]any) (subagentctl.WaitRequest, error) {
	for key := range args {
		if key != "ids" && key != "timeout_seconds" {
			return subagentctl.WaitRequest{}, argsError("unknown parameter: " + key)
		}
	}
	rawIDs, ok := args["ids"].([]any)
	if !ok || len(rawIDs) == 0 {
		return subagentctl.WaitRequest{}, argsError("ids must be a non-empty array of UUID strings")
	}
	ids := make([]uuid.UUID, 0, len(rawIDs))
	for _, raw := range rawIDs {
		s, ok := raw.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return subagentctl.WaitRequest{}, argsError("each id must be a valid UUID string")
		}
		id, err := uuid.Parse(strings.TrimSpace(s))
		if err != nil {
			return subagentctl.WaitRequest{}, argsError("each id must be a valid UUID string")
		}
		ids = append(ids, id)
	}
	var timeout time.Duration
	if raw, ok := args["timeout_seconds"]; ok {
		seconds, err := parsePositiveInt(raw)
		if err != nil {
			return subagentctl.WaitRequest{}, argsError("timeout_seconds must be a positive integer")
		}
		timeout = time.Duration(seconds) * time.Second
	}
	return subagentctl.WaitRequest{SubAgentIDs: ids, Timeout: timeout}, nil
}

func parseInterruptArgs(args map[string]any) (subagentctl.InterruptRequest, error) {
	subAgentID, err := parseSubAgentIDArg(args)
	if err != nil {
		return subagentctl.InterruptRequest{}, err
	}
	reason := ""
	if raw, ok := args["reason"]; ok {
		value, ok := raw.(string)
		if !ok {
			return subagentctl.InterruptRequest{}, argsError("reason must be a string")
		}
		reason = strings.TrimSpace(value)
	}
	return subagentctl.InterruptRequest{SubAgentID: subAgentID, Reason: reason}, nil
}

func parseSubAgentIDArg(args map[string]any) (uuid.UUID, error) {
	for key := range args {
		if key != "sub_agent_id" && key != "input" && key != "interrupt" && key != "reason" {
			return uuid.Nil, argsError("unknown parameter: " + key)
		}
	}
	raw, ok := args["sub_agent_id"].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return uuid.Nil, argsError("sub_agent_id must be a non-empty string")
	}
	subAgentID, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		return uuid.Nil, argsError("sub_agent_id must be a valid UUID")
	}
	return subAgentID, nil
}

func parsePositiveInt(raw any) (int, error) {
	switch value := raw.(type) {
	case int:
		if value <= 0 {
			return 0, fmt.Errorf("invalid")
		}
		return value, nil
	case int64:
		if value <= 0 {
			return 0, fmt.Errorf("invalid")
		}
		return int(value), nil
	case float64:
		if value <= 0 || value != float64(int(value)) {
			return 0, fmt.Errorf("invalid")
		}
		return int(value), nil
	default:
		return 0, fmt.Errorf("invalid")
	}
}

func parseSpawnInherit(raw any) (subagentctl.SpawnInheritRequest, error) {
	obj, ok := raw.(map[string]any)
	if !ok {
		return subagentctl.SpawnInheritRequest{}, argsError("inherit must be an object")
	}
	result := subagentctl.SpawnInheritRequest{}
	for key, value := range obj {
		switch key {
		case "messages":
			parsed, err := parseOptionalBool(value, "inherit.messages")
			if err != nil {
				return subagentctl.SpawnInheritRequest{}, err
			}
			result.Messages = parsed
		case "attachments":
			parsed, err := parseOptionalBool(value, "inherit.attachments")
			if err != nil {
				return subagentctl.SpawnInheritRequest{}, err
			}
			result.Attachments = parsed
		case "workspace":
			parsed, err := parseOptionalBool(value, "inherit.workspace")
			if err != nil {
				return subagentctl.SpawnInheritRequest{}, err
			}
			result.Workspace = parsed
		case "skills":
			parsed, err := parseOptionalBool(value, "inherit.skills")
			if err != nil {
				return subagentctl.SpawnInheritRequest{}, err
			}
			result.Skills = parsed
		case "runtime":
			parsed, err := parseOptionalBool(value, "inherit.runtime")
			if err != nil {
				return subagentctl.SpawnInheritRequest{}, err
			}
			result.Runtime = parsed
		case "memory_scope":
			text, ok := value.(string)
			if !ok || strings.TrimSpace(text) == "" {
				return subagentctl.SpawnInheritRequest{}, argsError("inherit.memory_scope must be a non-empty string")
			}
			result.MemoryScope = strings.TrimSpace(text)
		case "message_ids":
			items, ok := value.([]any)
			if !ok || len(items) == 0 {
				return subagentctl.SpawnInheritRequest{}, argsError("inherit.message_ids must be a non-empty string array")
			}
			seen := map[uuid.UUID]struct{}{}
			for _, item := range items {
				text, ok := item.(string)
				if !ok || strings.TrimSpace(text) == "" {
					return subagentctl.SpawnInheritRequest{}, argsError("inherit.message_ids must contain valid UUID strings")
				}
				messageID, err := uuid.Parse(strings.TrimSpace(text))
				if err != nil {
					return subagentctl.SpawnInheritRequest{}, argsError("inherit.message_ids must contain valid UUID strings")
				}
				if _, ok := seen[messageID]; ok {
					continue
				}
				seen[messageID] = struct{}{}
				result.MessageIDs = append(result.MessageIDs, messageID)
			}
		default:
			return subagentctl.SpawnInheritRequest{}, argsError("unknown inherit parameter: " + key)
		}
	}
	return result, nil
}

func parseOptionalBool(raw any, field string) (*bool, error) {
	value, ok := raw.(bool)
	if !ok {
		return nil, argsError(field + " must be a boolean")
	}
	return &value, nil
}

func snapshotJSON(snapshot subagentctl.StatusSnapshot) map[string]any {
	result := map[string]any{
		"sub_agent_id":  snapshot.SubAgentID.String(),
		"parent_run_id": snapshot.ParentRunID.String(),
		"root_run_id":   snapshot.RootRunID.String(),
		"depth":         snapshot.Depth,
		"status":        snapshot.Status,
	}
	if snapshot.PersonaID != nil {
		result["persona_id"] = *snapshot.PersonaID
	}
	if snapshot.Role != nil {
		result["role"] = *snapshot.Role
	}
	if snapshot.Nickname != nil {
		result["nickname"] = *snapshot.Nickname
	}
	if strings.TrimSpace(snapshot.ContextMode) != "" {
		result["context_mode"] = snapshot.ContextMode
	}
	if snapshot.CurrentRunID != nil {
		result["current_run_id"] = snapshot.CurrentRunID.String()
	}
	if snapshot.LastCompletedRunID != nil {
		result["last_completed_run_id"] = snapshot.LastCompletedRunID.String()
	}
	if snapshot.LastOutputRef != nil {
		result["last_output_ref"] = *snapshot.LastOutputRef
	}
	if snapshot.LastOutput != nil {
		result["output"] = *snapshot.LastOutput
	}
	if snapshot.LastError != nil {
		result["last_error"] = *snapshot.LastError
	}
	if snapshot.LastEventSeq != nil {
		result["last_event_seq"] = *snapshot.LastEventSeq
	}
	if snapshot.LastEventType != nil {
		result["last_event_type"] = *snapshot.LastEventType
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func argsError(message string) *tools.ExecutionError {
	return &tools.ExecutionError{ErrorClass: errorArgsInvalid, Message: message}
}

func errorResult(errorClass string, message string, started time.Time) tools.ExecutionResult {
	return tools.ExecutionResult{
		Error:      &tools.ExecutionError{ErrorClass: errorClass, Message: message},
		DurationMs: durationMs(started),
	}
}

func isTimeoutError(err error) bool {
	return err == context.DeadlineExceeded || err == context.Canceled
}

func stringPtr(s string) *string { return &s }

func durationMs(started time.Time) int {
	ms := int(time.Since(started) / time.Millisecond)
	if ms < 0 {
		return 0
	}
	return ms
}

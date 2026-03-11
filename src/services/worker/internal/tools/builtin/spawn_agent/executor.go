package spawnagent

import (
	"context"
	"fmt"
	"strings"
	"time"

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
			"persona_id": map[string]any{"type": "string"},
			"input":      map[string]any{"type": "string"},
		},
		"required":             []string{"persona_id", "input"},
		"additionalProperties": false,
	},
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
			"sub_agent_id":    map[string]any{"type": "string"},
			"timeout_seconds": map[string]any{"type": "integer", "minimum": 1},
		},
		"required":             []string{"sub_agent_id"},
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
	Control subagentctl.Control
}

func (e *ToolExecutor) Execute(
	ctx context.Context,
	toolName string,
	args map[string]any,
	_ tools.ExecutionContext,
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
		var personaID, input string
		personaID, input, err = parseSpawnArgs(args)
		if err == nil {
			snapshot, err = e.Control.Spawn(ctx, subagentctl.SpawnRequest{PersonaID: personaID, Input: input})
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
		if isTimeoutError(err) {
			return errorResult(errorTimeout, err.Error(), started)
		}
		return errorResult(errorControlFailed, err.Error(), started)
	}
	return tools.ExecutionResult{ResultJSON: snapshotJSON(snapshot), DurationMs: durationMs(started)}
}

func parseSpawnArgs(args map[string]any) (string, string, error) {
	for key := range args {
		if key != "persona_id" && key != "input" {
			return "", "", argsError("unknown parameter: " + key)
		}
	}
	personaID, ok := args["persona_id"].(string)
	if !ok || strings.TrimSpace(personaID) == "" {
		return "", "", argsError("persona_id must be a non-empty string")
	}
	input, ok := args["input"].(string)
	if !ok || strings.TrimSpace(input) == "" {
		return "", "", argsError("input must be a non-empty string")
	}
	return strings.TrimSpace(personaID), strings.TrimSpace(input), nil
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
	subAgentID, err := parseSubAgentIDArg(args)
	if err != nil {
		return subagentctl.WaitRequest{}, err
	}
	var timeout time.Duration
	if raw, ok := args["timeout_seconds"]; ok {
		seconds, err := parsePositiveInt(raw)
		if err != nil {
			return subagentctl.WaitRequest{}, argsError("timeout_seconds must be a positive integer")
		}
		timeout = time.Duration(seconds) * time.Second
	}
	return subagentctl.WaitRequest{SubAgentID: subAgentID, Timeout: timeout}, nil
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
		if key != "sub_agent_id" && key != "timeout_seconds" && key != "input" && key != "interrupt" && key != "reason" {
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
	if snapshot.Nickname != nil {
		result["nickname"] = *snapshot.Nickname
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

package platform

import (
"context"
"fmt"
"strings"
"time"

"arkloop/services/worker/internal/llm"
"arkloop/services/worker/internal/subagentctl"
"arkloop/services/worker/internal/tools"

"github.com/google/uuid"
)

const platformPersonaID = "platform"

var CallPlatformAgentSpec = tools.AgentToolSpec{
Name:        "call_platform",
Version:     "1",
Description: "delegate a platform management task to the Platform Agent",
RiskLevel:   tools.RiskLevelHigh,
SideEffects: true,
}

var CallPlatformLlmSpec = llm.ToolSpec{
Name:        CallPlatformAgentSpec.Name,
Description: sp("Delegate a platform management task (settings, providers, personas, skills, MCP, access control, infrastructure) to the Platform Agent. The task is described in natural language and executed synchronously."),
JSONSchema: map[string]any{
"type": "object",
"properties": map[string]any{
"task": map[string]any{
"type":        "string",
"description": "Natural language description of the platform management task to perform.",
},
},
"required":             []string{"task"},
"additionalProperties": false,
},
}


// CallPlatformExecutor 通过 SubAgentControl 派生一个 platform persona 子 agent 来执行管理任务。
// 过渡期复用 spawnChildRun，identity 注入在 PR-7 补齐。
type CallPlatformExecutor struct {
Control subagentctl.Control
}

func (e *CallPlatformExecutor) Execute(
ctx context.Context,
toolName string,
args map[string]any,
_ tools.ExecutionContext,
_ string,
) tools.ExecutionResult {
started := time.Now()

if e.Control == nil {
return tools.ExecutionResult{
Error:      &tools.ExecutionError{ErrorClass: "tool.not_initialized", Message: "call_platform not available"},
DurationMs: ms(started),
}
}

task, ok := args["task"].(string)
if !ok || strings.TrimSpace(task) == "" {
return tools.ExecutionResult{
Error:      &tools.ExecutionError{ErrorClass: "tool.args_invalid", Message: "task is required"},
DurationMs: ms(started),
}
}

snapshot, err := e.Control.Spawn(ctx, subagentctl.SpawnRequest{
PersonaID:   platformPersonaID,
ContextMode: "isolated",
Input:       task,
SourceType:  "platform_agent",
})
if err != nil {
return tools.ExecutionResult{
Error:      &tools.ExecutionError{ErrorClass: "tool.spawn_failed", Message: fmt.Sprintf("failed to spawn platform agent: %v", err)},
DurationMs: ms(started),
}
}

if snapshot.SubAgentID == uuid.Nil {
return tools.ExecutionResult{
Error:      &tools.ExecutionError{ErrorClass: "tool.spawn_failed", Message: "platform agent returned empty sub_agent_id"},
DurationMs: ms(started),
}
}

// 同步等待 platform agent 完成
waitResult, err := e.Control.Wait(ctx, subagentctl.WaitRequest{
SubAgentIDs: []uuid.UUID{snapshot.SubAgentID},
Timeout:     5 * time.Minute,
})
if err != nil {
result := map[string]any{
"sub_agent_id": snapshot.SubAgentID.String(),
"status":       "timeout",
"message":      fmt.Sprintf("platform agent did not complete within timeout: %v", err),
}
return tools.ExecutionResult{ResultJSON: result, DurationMs: ms(started)}
}

result := map[string]any{
"sub_agent_id": waitResult.SubAgentID.String(),
"status":       waitResult.Status,
}
if waitResult.LastOutput != nil {
result["output"] = *waitResult.LastOutput
}
if waitResult.LastError != nil {
result["error"] = *waitResult.LastError
}
return tools.ExecutionResult{ResultJSON: result, DurationMs: ms(started)}
}

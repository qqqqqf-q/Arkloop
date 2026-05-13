package exit_plan_mode

import (
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

const ToolName = "exit_plan_mode"

var AgentSpec = tools.AgentToolSpec{
	Name:        ToolName,
	Version:     "1",
	Description: "响应用户批准或 Build 动作，标记当前 Plan Mode 已绑定的计划可执行。",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: true,
}

var LlmSpec = llm.ToolSpec{
	Name:        ToolName,
	Description: strPtr("仅在用户明确批准、点击 Build，或系统消息要求执行计划时调用。不要在刚写完计划后主动调用。调用前应已创建并绑定当前 plan 文件，且文件应以 YAML front matter 开头，包含 name、overview、todos、isProject。本工具只把线程从 Plan Mode 切回普通执行模式；工具成功后必须继续按已批准 plan 执行实际工作，不要把调用本工具当成执行完成。"),
	JSONSchema: map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	},
}

func strPtr(s string) *string { return &s }

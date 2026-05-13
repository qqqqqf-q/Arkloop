package enter_plan_mode

import (
	"arkloop/services/worker/internal/llm"
	"arkloop/services/worker/internal/tools"
)

const ToolName = "enter_plan_mode"

var AgentSpec = tools.AgentToolSpec{
	Name:        ToolName,
	Version:     "1",
	Description: "进入 Plan Mode：在计划目录创建或维护计划文件，再交给用户确认。",
	RiskLevel:   tools.RiskLevelLow,
	SideEffects: true,
}

var LlmSpec = llm.ToolSpec{
	Name:        ToolName,
	Description: strPtr("进入 Plan Mode。不要修改普通项目文件。新计划应在计划目录直接创建一个语义化名称加短随机后缀的 .plan.md 文件，不要先读取一个不存在的预设文件；写好或改好后，在 assistant 回复中用 Markdown resource link 引用该文件以展示给用户。只有用户明确要求继续或修改已有计划时才读取已有 plan 文件。不要主动调用 exit_plan_mode；它只响应用户批准、Build/执行动作或系统指令。exit_plan_mode 成功后应继续按已批准计划执行实际工作。"),
	JSONSchema: map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	},
}

func strPtr(s string) *string { return &s }

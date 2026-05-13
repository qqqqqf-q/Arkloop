package enter_plan_mode

import (
	"context"
	"time"

	"arkloop/services/worker/internal/events"
	"arkloop/services/worker/internal/tools"
)

type PipelineBinding interface {
	SetIsPlanMode(active bool)
	IsPlanModeActive() bool
}

type executor struct{}

func New() tools.Executor {
	return executor{}
}

func (executor) Execute(
	ctx context.Context,
	toolName string,
	_ map[string]any,
	execCtx tools.ExecutionContext,
	_ string,
) tools.ExecutionResult {
	started := time.Now()
	if toolName != ToolName {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: tools.ErrorClassToolExecutionFailed,
				Message:    "unexpected tool name",
			},
			DurationMs: int(time.Since(started).Milliseconds()),
		}
	}

	binding, ok := execCtx.PipelineRC.(PipelineBinding)
	if !ok || binding == nil {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: tools.ErrorClassToolExecutionFailed,
				Message:    "enter_plan_mode: pipeline binding unavailable",
			},
			DurationMs: int(time.Since(started).Milliseconds()),
		}
	}

	if execCtx.ThreadID == nil {
		return tools.ExecutionResult{
			Error: &tools.ExecutionError{
				ErrorClass: tools.ErrorClassToolExecutionFailed,
				Message:    "enter_plan_mode: thread_id is required",
			},
			DurationMs: int(time.Since(started).Milliseconds()),
		}
	}
	planDir := tools.DefaultPlanDirectory()
	if binding.IsPlanModeActive() {
		return tools.ExecutionResult{
			ResultJSON: map[string]any{
				"status":         "already_in_plan",
				"plan_directory": planDir,
			},
			DurationMs: int(time.Since(started).Milliseconds()),
		}
	}

	binding.SetIsPlanMode(true)

	instructions := "你已进入 Plan Mode。\n\n" +
		"当前 Plan Mode 先绑定到计划目录：" + planDir + "。\n" +
		"新计划请直接在该目录创建一个语义化名称加短随机后缀的 .plan.md 文件，不要先读取一个不存在的默认文件。\n" +
		"写好或改好 Plan 后，在 assistant 回复中用 Markdown resource link 引用这个文件，例如 [计划名称](file:///absolute/path/to/example_0cc67a18.plan.md)，让右侧文档面板渲染它；不要依赖 tool result 自动展示。\n" +
		"可以读取代码、搜索、提问并修订 Plan 文件；不要修改普通项目文件或执行会改变项目状态的命令。不要主动调用 exit_plan_mode；它只响应用户批准、Build/执行动作或系统指令。exit_plan_mode 成功后继续按已批准 Plan 执行，不要把退出 Plan Mode 当成执行完成。"

	event := execCtx.Emitter.Emit("thread.collaboration_mode.updated", map[string]any{
		"thread_id":                   execCtx.ThreadID.String(),
		"run_id":                      execCtx.RunID.String(),
		"previous_collaboration_mode": "default",
		"collaboration_mode":          "plan",
		"plan_directory":              planDir,
		"source":                      "model",
	}, nil, nil)

	return tools.ExecutionResult{
		ResultJSON: map[string]any{
			"status":         "plan_mode_entered",
			"plan_directory": planDir,
			"instructions":   instructions,
		},
		DurationMs: int(time.Since(started).Milliseconds()),
		Events:     []events.RunEvent{event},
	}
}

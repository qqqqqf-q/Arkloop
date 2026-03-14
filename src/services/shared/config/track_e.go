package config

import "fmt"

func RegisterTrackE(r *Registry) error {
	if r == nil {
		return fmt.Errorf("registry must not be nil")
	}

	entries := []Entry{
		{
			Key:         "limit.subagent_max_depth",
			Type:        TypeInt,
			Default:     "5",
			Description: "Sub-Agent 最大嵌套深度",
			Sensitive:   false,
			Scope:       ScopeBoth,
		},
		{
			Key:         "limit.subagent_max_active_per_root_run",
			Type:        TypeInt,
			Default:     "20",
			Description: "单 root run 下最大活跃 sub-agent 数量",
			Sensitive:   false,
			Scope:       ScopeBoth,
		},
		{
			Key:         "limit.subagent_max_parallel_children",
			Type:        TypeInt,
			Default:     "5",
			Description: "单 run 下最大并行子 agent 数量",
			Sensitive:   false,
			Scope:       ScopeBoth,
		},
		{
			Key:         "limit.subagent_max_descendants_per_root_run",
			Type:        TypeInt,
			Default:     "50",
			Description: "单 root run 下 sub-agent 总数上限",
			Sensitive:   false,
			Scope:       ScopeBoth,
		},
		{
			Key:         "limit.subagent_max_pending_per_root_run",
			Type:        TypeInt,
			Default:     "20",
			Description: "单 root run 下待处理输入队列上限",
			Sensitive:   false,
			Scope:       ScopeBoth,
		},
		{
			Key:         "memory.distill_enabled",
			Type:        TypeBool,
			Default:     "true",
			Description: "启用 run 结束后的自动 Memory 提炼",
			Sensitive:   false,
			Scope:       ScopeBoth,
		},
		{
			Key:         "memory.distill_min_tool_calls",
			Type:        TypeInt,
			Default:     "2",
			Description: "触发 Memory 提炼的最低 tool call 次数",
			Sensitive:   false,
			Scope:       ScopeBoth,
		},
		{
			Key:         "memory.distill_min_rounds",
			Type:        TypeInt,
			Default:     "3",
			Description: "触发 Memory 提炼的最低 LLM 迭代轮数",
			Sensitive:   false,
			Scope:       ScopeBoth,
		},
		{
			Key:         "backpressure.enabled",
			Type:        TypeBool,
			Default:     "true",
			Description: "启用 sub-agent 背压治理",
			Sensitive:   false,
			Scope:       ScopeBoth,
		},
		{
			Key:         "backpressure.queue_threshold",
			Type:        TypeInt,
			Default:     "15",
			Description: "单 root run 下触发背压的活跃 sub-agent 数量阈值",
			Sensitive:   false,
			Scope:       ScopeBoth,
		},
		{
			Key:         "backpressure.strategy",
			Type:        TypeString,
			Default:     "serial",
			Description: "背压降级策略: serial/reject/pause",
			Sensitive:   false,
			Scope:       ScopeBoth,
		},
		{
			Key:         "budget.max_cost_micros",
			Type:        TypeInt,
			Default:     "0",
			Description: "单次 run 最大累计费用 (微美元), 0 表示不限",
			Sensitive:   false,
			Scope:       ScopeBoth,
		},
		{
			Key:         "budget.max_total_output_tokens",
			Type:        TypeInt,
			Default:     "0",
			Description: "单次 run 最大累计输出 token 数, 0 表示不限",
			Sensitive:   false,
			Scope:       ScopeBoth,
		},
		{
			Key:         "spawn.profile.fast",
			Type:        TypeString,
			Default:     "anthropic^claude-haiku-3-5",
			Description: "ACP agent 'fast' profile: 低延迟低成本模型",
			Sensitive:   false,
			Scope:       ScopeBoth,
		},
		{
			Key:         "spawn.profile.balanced",
			Type:        TypeString,
			Default:     "anthropic^claude-sonnet-4-5",
			Description: "ACP agent 'balanced' profile: 平衡性价比模型",
			Sensitive:   false,
			Scope:       ScopeBoth,
		},
		{
			Key:         "spawn.profile.strong",
			Type:        TypeString,
			Default:     "anthropic^claude-sonnet-4-5",
			Description: "ACP agent 'strong' profile: 最强推理能力模型",
			Sensitive:   false,
			Scope:       ScopeBoth,
		},
	}

	for _, e := range entries {
		if err := r.Register(e); err != nil {
			return err
		}
	}
	return nil
}

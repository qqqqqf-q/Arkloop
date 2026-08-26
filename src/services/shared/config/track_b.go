package config

import (
	"fmt"
)

func RegisterTrackB(r *Registry) error {
	if r == nil {
		return fmt.Errorf("registry must not be nil")
	}

	entries := []Entry{

		{
			Key:         "feature.mcp_remote_enabled",
			Type:        TypeBool,
			Default:     "false",
			Description: "是否允许远程 MCP",
			Sensitive:   false,
			Scope:       ScopeBoth,
		},

		{
			Key:         "invite.max_codes_per_user",
			Type:        TypeInt,
			Default:     "1",
			Description: "单用户可创建的邀请码数量上限",
			Sensitive:   false,
			Scope:       ScopeBoth,
		},

		{
			Key:         "limit.agent_reasoning_iterations",
			Type:        TypeInt,
			Default:     "0",
			Description: "Agent Loop 主推理回合上限，0 表示不限",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "limit.tool_continuation_budget",
			Type:        TypeInt,
			Default:     "32",
			Description: "长工具 continuation 总预算上限",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "limit.max_input_content_bytes",
			Type:        TypeInt,
			Default:     "32768",
			Description: "Run input 提交内容最大字节数",
			Sensitive:   false,
			Scope:       ScopeBoth,
		},
		{
			Key:         "limit.max_parallel_tasks",
			Type:        TypeInt,
			Default:     "32",
			Description: "Lua 并行任务/并行工具调用上限",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "limit.run_idle_timeout_ms",
			Type:        TypeInt,
			Default:     "900000",
			Description: "单个 run 无有效进展的超时时间（毫秒）",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "limit.run_wall_clock_timeout_ms",
			Type:        TypeInt,
			Default:     "14400000",
			Description: "单个 run 的最大 wall clock 硬截止时间（毫秒）",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "limit.paused_input_timeout_ms",
			Type:        TypeInt,
			Default:     "300000",
			Description: "run 进入等待用户输入后的超时时间（毫秒）",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "limit.idle_heartbeat_interval_ms",
			Type:        TypeInt,
			Default:     "15000",
			Description: "长等待期间发出活跃事件的心跳间隔（毫秒）",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "context.compact.enabled",
			Type:        TypeBool,
			Default:     "true",
			Description: "启用线程上下文预算裁切（在 Routing 之后）",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "context.compact.max_messages",
			Type:        TypeInt,
			Default:     "0",
			Description: "compact 尾部消息条数上限，0 表示仅按 token/字节预算",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "context.compact.max_user_message_tokens",
			Type:        TypeInt,
			Default:     "0",
			Description: "保留 user 的 tiktoken 累计上限（role+正文），0 表示不限制",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "context.compact.max_total_text_tokens",
			Type:        TypeInt,
			Default:     "0",
			Description: "全消息 tiktoken 累计上限（role+正文），0 表示不限制",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "context.compact.max_user_text_bytes",
			Type:        TypeInt,
			Default:     "0",
			Description: "保留 user 文本字节上限，0 表示不限制",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "context.compact.max_total_text_bytes",
			Type:        TypeInt,
			Default:     "0",
			Description: "全消息文本字节上限，0 表示不限制",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "context.compact.persist_trigger_context_pct",
			Type:        TypeInt,
			Default:     "80",
			Description: "按路由 available_catalog.context_length（否则 fallback）的百分比触发 compact",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "context.compact.target_context_pct",
			Type:        TypeInt,
			Default:     "75",
			Description: "compact 循环压回的上下文窗口百分比目标",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "context.compact.fallback_context_window_tokens",
			Type:        TypeInt,
			Default:     "128000",
			Description: "路由 advanced_json 未解析出上下文窗口时用于百分比换算的窗口上限",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "llm.max_response_bytes",
			Type:        TypeInt,
			Default:     "16384",
			Description: "LLM Provider HTTP 响应读取上限（字节）",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "skills.registry.provider",
			Type:        TypeString,
			Default:     "clawhub",
			Description: "官方技能 Registry Provider",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "skills.registry.base_url",
			Type:        TypeString,
			Default:     "https://clawhub.ai",
			Description: "官方技能 Registry 页面基础地址",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "skills.registry.api_base_url",
			Type:        TypeString,
			Default:     "",
			Description: "官方技能 Registry API 基础地址，留空则沿用 Base URL",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "skills.registry.api_key",
			Type:        TypeString,
			Default:     "",
			Description: "官方技能 Registry API Key",
			Sensitive:   true,
			Scope:       ScopePlatform,
		},
		{
			Key:         "skills.market.skillsmp_api_key",
			Type:        TypeString,
			Default:     "",
			Description: "SkillsMP 官方市场 API Key",
			Sensitive:   true,
			Scope:       ScopePlatform,
		},
		{
			Key:         "skills.market.skillsmp_base_url",
			Type:        TypeString,
			Default:     "https://skillsmp.com",
			Description: "SkillsMP 官方市场基础地址",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
	}

	for _, e := range entries {
		if err := r.Register(e); err != nil {
			return err
		}
	}
	return nil
}

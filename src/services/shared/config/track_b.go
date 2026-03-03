package config

import (
	"fmt"

	"arkloop/services/shared/creditpolicy"
)

func RegisterTrackB(r *Registry) error {
	if r == nil {
		return fmt.Errorf("registry must not be nil")
	}

	entries := []Entry{
		{
			Key:         "browser.context_max_lifetime_s",
			Type:        TypeInt,
			Default:     "1800",
			Description: "Browser Context 最大存活时间（秒）",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "browser.max_body_bytes",
			Type:        TypeInt,
			Default:     "1048576",
			Description: "Browser Service 请求体大小上限（字节）",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},

		{
			Key:         "credit.deduction_policy",
			Type:        TypeString,
			Default:     creditpolicy.DefaultPolicyJSON,
			Description: "积分扣减策略（JSON）",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "credit.initial_grant",
			Type:        TypeInt,
			Default:     "1000",
			Description: "新组织初始积分发放数量",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "credit.invite_reward",
			Type:        TypeInt,
			Default:     "500",
			Description: "邀请者奖励积分数量",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "credit.invitee_reward",
			Type:        TypeInt,
			Default:     "200",
			Description: "被邀请者奖励积分数量",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "credit.per_usd",
			Type:        TypeInt,
			Default:     "1000",
			Description: "积分汇率：每 1 USD 对应积分数",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},

		{
			Key:         "feature.byok_enabled",
			Type:        TypeBool,
			Default:     "true",
			Description: "是否允许使用 org 级凭证（BYOK）",
			Sensitive:   false,
			Scope:       ScopeBoth,
		},
		{
			Key:         "feature.mcp_remote_enabled",
			Type:        TypeBool,
			Default:     "false",
			Description: "是否允许远程 MCP",
			Sensitive:   false,
			Scope:       ScopeBoth,
		},

		{
			Key:         "invite.default_max_uses",
			Type:        TypeInt,
			Default:     "1",
			Description: "邀请码默认可用次数",
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
			Key:         "limit.agent_max_iterations",
			Type:        TypeInt,
			Default:     "10",
			Description: "Agent Loop 最大迭代次数上限",
			Sensitive:   false,
			Scope:       ScopeBoth,
		},
		{
			Key:         "limit.concurrent_runs",
			Type:        TypeInt,
			Default:     "10",
			Description: "并发 run 上限",
			Sensitive:   false,
			Scope:       ScopeBoth,
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
			Key:         "limit.team_members",
			Type:        TypeInt,
			Default:     "50",
			Description: "Team 成员数量上限",
			Sensitive:   false,
			Scope:       ScopeBoth,
		},
		{
			Key:         "limit.thread_message_history",
			Type:        TypeInt,
			Default:     "200",
			Description: "线程历史消息加载上限（条）",
			Sensitive:   false,
			Scope:       ScopeBoth,
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
			Key:         "quota.runs_per_month",
			Type:        TypeInt,
			Default:     "999999",
			Description: "每月 run 数量配额",
			Sensitive:   false,
			Scope:       ScopeBoth,
		},
		{
			Key:         "quota.tokens_per_month",
			Type:        TypeInt,
			Default:     "1000000",
			Description: "每月 token 配额",
			Sensitive:   false,
			Scope:       ScopeBoth,
		},

		{
			Key:         "sandbox.base_url",
			Type:        TypeString,
			Default:     "",
			Description: "Sandbox Service 地址，Worker 通过此 URL 调用 Sandbox；为空则不注册 sandbox 工具",
			Sensitive:   false,
			Scope:       ScopePlatform,
			EnvKeys:     []string{"ARKLOOP_SANDBOX_BASE_URL"},
		},
		{
			Key:         "sandbox.provider",
			Type:        TypeString,
			Default:     "firecracker",
			Description: "Sandbox 后端类型：firecracker / docker",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "sandbox.docker_image",
			Type:        TypeString,
			Default:     "arkloop/sandbox-agent:latest",
			Description: "Docker 后端使用的 sandbox-agent 镜像",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "sandbox.max_sessions",
			Type:        TypeInt,
			Default:     "50",
			Description: "Sandbox 最大并发 session 数",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "sandbox.agent_port",
			Type:        TypeInt,
			Default:     "8080",
			Description: "Sandbox Agent 监听端口",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "sandbox.boot_timeout_s",
			Type:        TypeInt,
			Default:     "30",
			Description: "VM/容器启动超时（秒）",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "sandbox.warm_lite",
			Type:        TypeInt,
			Default:     "3",
			Description: "lite tier 预热实例数",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "sandbox.warm_pro",
			Type:        TypeInt,
			Default:     "2",
			Description: "pro tier 预热实例数",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "sandbox.warm_ultra",
			Type:        TypeInt,
			Default:     "1",
			Description: "ultra tier 预热实例数",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "sandbox.refill_interval_s",
			Type:        TypeInt,
			Default:     "5",
			Description: "预热补充检查间隔（秒）",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},
		{
			Key:         "sandbox.refill_concurrency",
			Type:        TypeInt,
			Default:     "2",
			Description: "预热补充最大并发数",
			Sensitive:   false,
			Scope:       ScopePlatform,
		},

		{
			Key:         "sandbox.idle_timeout_lite_s",
			Type:        TypeInt,
			Default:     "180",
			Description: "Sandbox lite tier 空闲超时（秒）",
			Sensitive:   false,
			Scope:       ScopePlatform,
			EnvKeys:     []string{"ARKLOOP_SANDBOX_IDLE_TIMEOUT_LITE_S", "ARKLOOP_SANDBOX_IDLE_TIMEOUT_LITE"},
		},
		{
			Key:         "sandbox.idle_timeout_pro_s",
			Type:        TypeInt,
			Default:     "300",
			Description: "Sandbox pro tier 空闲超时（秒）",
			Sensitive:   false,
			Scope:       ScopePlatform,
			EnvKeys:     []string{"ARKLOOP_SANDBOX_IDLE_TIMEOUT_PRO_S", "ARKLOOP_SANDBOX_IDLE_TIMEOUT_PRO"},
		},
		{
			Key:         "sandbox.idle_timeout_ultra_s",
			Type:        TypeInt,
			Default:     "600",
			Description: "Sandbox ultra tier 空闲超时（秒）",
			Sensitive:   false,
			Scope:       ScopePlatform,
			EnvKeys:     []string{"ARKLOOP_SANDBOX_IDLE_TIMEOUT_ULTRA_S", "ARKLOOP_SANDBOX_IDLE_TIMEOUT_ULTRA"},
		},
		{
			Key:         "sandbox.max_lifetime_s",
			Type:        TypeInt,
			Default:     "1800",
			Description: "Sandbox session 最大存活时间（秒）",
			Sensitive:   false,
			Scope:       ScopePlatform,
			EnvKeys:     []string{"ARKLOOP_SANDBOX_MAX_LIFETIME_S", "ARKLOOP_SANDBOX_MAX_LIFETIME"},
		},
	}

	for _, e := range entries {
		if err := r.Register(e); err != nil {
			return err
		}
	}
	return nil
}

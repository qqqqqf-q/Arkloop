| key | type | scope | default | sensitive | description |
| --- | --- | --- | --- | --- | --- |
| browser.context_max_lifetime_s | int | platform | 1800 | false | Browser Context 最大存活时间（秒） |
| browser.max_body_bytes | int | platform | 1048576 | false | Browser Service 请求体大小上限（字节） |
| credit.deduction_policy | string | platform | {"tiers":[{"up_to_tokens":2000,"multiplier":0},{"multiplier":1}]} | false | 积分扣减策略（JSON） |
| credit.initial_grant | int | platform | 1000 | false | 新组织初始积分发放数量 |
| credit.invite_reward | int | platform | 500 | false | 邀请者奖励积分数量 |
| credit.invitee_reward | int | platform | 200 | false | 被邀请者奖励积分数量 |
| credit.per_usd | int | platform | 1000 | false | 积分汇率：每 1 USD 对应积分数 |
| email.from | string | platform |  | false | SMTP 发件人地址，留空表示禁用邮件发送 |
| email.smtp_host | string | platform |  | false | SMTP Host |
| email.smtp_pass | string | platform |  | true | SMTP 密码 |
| email.smtp_port | int | platform | 587 | false | SMTP 端口 |
| email.smtp_tls_mode | string | platform | starttls | false | SMTP TLS 模式：starttls/tls/none |
| email.smtp_user | string | platform |  | false | SMTP 用户名 |
| feature.byok_enabled | bool | both | true | false | 是否允许使用 org 级凭证（BYOK） |
| feature.mcp_remote_enabled | bool | both | false | false | 是否允许远程 MCP |
| gateway.ip_mode | string | platform | direct | false | Gateway IP 模式：direct/cloudflare/trusted_proxy |
| gateway.ratelimit_capacity | number | platform | 600 | false | Gateway Rate Limit Capacity |
| gateway.ratelimit_rate_per_minute | number | platform | 300 | false | Gateway Rate Limit Per Minute |
| gateway.risk_reject_threshold | int | platform | 0 | false | Gateway 风险拒绝阈值（0-100） |
| gateway.trusted_cidrs | string | platform |  | false | Gateway 可信代理 CIDR 列表 |
| invite.default_max_uses | int | both | 1 | false | 邀请码默认可用次数 |
| invite.max_codes_per_user | int | both | 1 | false | 单用户可创建的邀请码数量上限 |
| limit.agent_reasoning_iterations | int | both | 10 | false | Agent Loop 主推理回合上限 |
| limit.concurrent_runs | int | both | 10 | false | 并发 run 上限 |
| limit.max_input_content_bytes | int | both | 32768 | false | Run input 提交内容最大字节数 |
| limit.max_parallel_tasks | int | platform | 32 | false | Lua 并行任务/并行工具调用上限 |
| limit.team_members | int | both | 50 | false | Team 成员数量上限 |
| limit.thread_message_history | int | both | 200 | false | 线程历史消息加载上限（条） |
| limit.tool_continuation_budget | int | both | 32 | false | 长工具 continuation 总预算上限 |
| llm.max_response_bytes | int | platform | 16384 | false | LLM Provider HTTP 响应读取上限（字节） |
| llm.retry.base_delay_ms | int | platform | 1000 | false | LLM 重试基础延迟（毫秒） |
| llm.retry.max_attempts | int | platform | 3 | false | LLM 重试最大次数 |
| openviking.base_url | string | platform |  | false | OpenViking Base URL |
| openviking.cost_per_commit | number | platform | 0 | false | OpenViking CommitSession Cost (USD) |
| openviking.root_api_key | string | platform |  | true | OpenViking Root API Key |
| quota.runs_per_month | int | both | 999999 | false | 每月 run 数量配额 |
| quota.tokens_per_month | int | both | 1000000 | false | 每月 token 配额 |
| sandbox.agent_port | int | platform | 8080 | false | Sandbox Agent 监听端口 |
| sandbox.base_url | string | platform |  | false | Sandbox Service 地址，Worker 通过此 URL 调用 Sandbox；为空则不注册 sandbox 工具 |
| sandbox.boot_timeout_s | int | platform | 30 | false | VM/容器启动超时（秒） |
| sandbox.credit_base_fee | int | platform | 1 | false | 每次 sandbox 调用的固定积分扣减，覆盖冷启动/调度开销 |
| sandbox.credit_rate_per_second | number | platform | 0.5 | false | sandbox 每秒执行时长对应的积分费率 |
| sandbox.docker_image | string | platform | arkloop/sandbox-agent:latest | false | Docker 后端使用的 sandbox-agent 镜像 |
| sandbox.idle_timeout_lite_s | int | platform | 180 | false | Sandbox lite tier 空闲超时（秒） |
| sandbox.idle_timeout_pro_s | int | platform | 300 | false | Sandbox pro tier 空闲超时（秒） |
| sandbox.idle_timeout_ultra_s | int | platform | 600 | false | Sandbox ultra tier 空闲超时（秒） |
| sandbox.max_lifetime_s | int | platform | 1800 | false | Sandbox session 最大存活时间（秒） |
| sandbox.max_sessions | int | platform | 50 | false | Sandbox 最大并发 session 数 |
| sandbox.provider | string | platform | firecracker | false | Sandbox 后端类型：firecracker / docker |
| sandbox.refill_concurrency | int | platform | 2 | false | 预热补充最大并发数 |
| sandbox.refill_interval_s | int | platform | 5 | false | 预热补充检查间隔（秒） |
| sandbox.warm_lite | int | platform | 3 | false | lite tier 预热实例数 |
| sandbox.warm_pro | int | platform | 2 | false | pro tier 预热实例数 |
| sandbox.warm_ultra | int | platform | 1 | false | ultra tier 预热实例数 |
| turnstile.allowed_host | string | platform |  | false | Turnstile Allowed Host |
| turnstile.secret_key | string | platform |  | true | Turnstile Secret Key |
| turnstile.site_key | string | platform |  | false | Turnstile Site Key |
| web_fetch.firecrawl_api_key | string | both |  | true | Firecrawl API Key |
| web_fetch.firecrawl_base_url | string | both |  | false | Firecrawl Base URL |
| web_fetch.jina_api_key | string | both |  | true | Jina API Key |
| web_fetch.provider | string | both | basic | false | Web Fetch Provider：basic/firecrawl/jina |
| web_search.provider | string | both |  | false | Web Search Provider：searxng/tavily |
| web_search.searxng_base_url | string | both |  | false | SearXNG Base URL |
| web_search.tavily_api_key | string | both |  | true | Tavily API Key |

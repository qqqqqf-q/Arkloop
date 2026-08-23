| key | type | scope | default | sensitive | description |
| --- | --- | --- | --- | --- | --- |
| backpressure.enabled | bool | both | true | false | 启用 sub-agent 背压治理 |
| backpressure.queue_threshold | int | both | 15 | false | 单 thread 下触发背压的活跃 sub-agent 数量阈值 |
| backpressure.strategy | string | both | serial | false | 背压降级策略: serial/reject/pause |
| browser.enabled | bool | platform | false | false | 是否在 Worker 中注册 browser 自动化工具 |
| budget.max_cost_micros | int | both | 0 | false | 单次 run 最大累计费用 (微美元), 0 表示不限 |
| budget.max_total_output_tokens | int | both | 0 | false | 单次 run 最大累计输出 token 数, 0 表示不限 |
| context.compact.enabled | bool | platform | true | false | 启用线程上下文预算裁切（在 Routing 之后） |
| context.compact.fallback_context_window_tokens | int | platform | 128000 | false | 路由 advanced_json 未解析出上下文窗口时用于百分比换算的窗口上限 |
| context.compact.max_messages | int | platform | 0 | false | compact 尾部消息条数上限，0 表示仅按 token/字节预算 |
| context.compact.max_total_text_bytes | int | platform | 0 | false | 全消息文本字节上限，0 表示不限制 |
| context.compact.max_total_text_tokens | int | platform | 0 | false | 全消息 tiktoken 累计上限（role+正文），0 表示不限制 |
| context.compact.max_user_message_tokens | int | platform | 0 | false | 保留 user 的 tiktoken 累计上限（role+正文），0 表示不限制 |
| context.compact.max_user_text_bytes | int | platform | 0 | false | 保留 user 文本字节上限，0 表示不限制 |
| context.compact.persist_trigger_context_pct | int | platform | 80 | false | 按路由 available_catalog.context_length（否则 fallback）的百分比触发 compact |
| context.compact.target_context_pct | int | platform | 75 | false | compact 循环压回的上下文窗口百分比目标 |
| email.from | string | platform |  | false | SMTP 发件人地址，留空表示禁用邮件发送 |
| email.smtp_host | string | platform |  | false | SMTP Host |
| email.smtp_pass | string | platform |  | true | SMTP 密码 |
| email.smtp_port | int | platform | 587 | false | SMTP 端口 |
| email.smtp_tls_mode | string | platform | starttls | false | SMTP TLS 模式：starttls/tls/none |
| email.smtp_user | string | platform |  | false | SMTP 用户名 |
| feature.mcp_remote_enabled | bool | both | false | false | 是否允许远程 MCP |
| image_generative.model | string | both |  | false | 默认图片生成模型，格式 provider^model |
| invite.max_codes_per_user | int | both | 1 | false | 单用户可创建的邀请码数量上限 |
| limit.agent_reasoning_iterations | int | platform | 0 | false | Agent Loop 主推理回合上限，0 表示不限 |
| limit.idle_heartbeat_interval_ms | int | platform | 15000 | false | 长等待期间发出活跃事件的心跳间隔（毫秒） |
| limit.max_input_content_bytes | int | both | 32768 | false | Run input 提交内容最大字节数 |
| limit.max_parallel_tasks | int | platform | 32 | false | Lua 并行任务/并行工具调用上限 |
| limit.paused_input_timeout_ms | int | platform | 300000 | false | run 进入等待用户输入后的超时时间（毫秒） |
| limit.run_idle_timeout_ms | int | platform | 900000 | false | 单个 run 无有效进展的超时时间（毫秒） |
| limit.run_wall_clock_timeout_ms | int | platform | 14400000 | false | 单个 run 的最大 wall clock 硬截止时间（毫秒） |
| limit.subagent_max_active_per_thread | int | both | 20 | false | 单 thread 下最大活跃 sub-agent 数量 |
| limit.subagent_max_depth | int | both | 5 | false | Sub-Agent 最大嵌套深度 |
| limit.subagent_max_descendants_per_thread | int | both | 50 | false | 单 thread 下 sub-agent 总数上限 |
| limit.subagent_max_parallel_children_per_thread | int | both | 5 | false | 单 thread 下最大并行子 agent 数量 |
| limit.subagent_max_pending_per_thread | int | both | 20 | false | 单 thread 下待处理输入队列上限 |
| limit.tool_continuation_budget | int | platform | 32 | false | 长工具 continuation 总预算上限 |
| llm.max_response_bytes | int | platform | 16384 | false | LLM Provider HTTP 响应读取上限（字节） |
| llm.retry.base_delay_ms | int | platform | 1000 | false | LLM 重试基础延迟（毫秒） |
| llm.retry.max_attempts | int | platform | 10 | false | LLM 重试最大次数 |
| memory.distill_enabled | bool | both | true | false | 启用普通对话在 run 结束后的自动 Memory 提炼 |
| memory.impression_score_threshold | int | both | 50 | false | impression 更新触发阈值 |
| nowledge.api_key | string | platform |  | true | Nowledge API Key |
| nowledge.base_url | string | platform |  | false | Nowledge Base URL |
| nowledge.max_context_results | int | platform | 5 | false | Max recalled memories injected per turn (1-20) |
| nowledge.recall_min_score | int | platform | 0 | false | Min recall score threshold 0-100 (0 = no filter) |
| nowledge.request_timeout_ms | int | platform | 30000 | false | Nowledge request timeout in milliseconds |
| sandbox.agent_port | int | platform | 8080 | false | Sandbox Agent 监听端口 |
| sandbox.allow_egress | bool | platform | true | false | Sandbox backend 是否允许访问外网 |
| sandbox.base_url | string | platform |  | false | Sandbox Service 地址，Worker 通过此 URL 调用 Sandbox；为空则不注册 sandbox 工具 |
| sandbox.boot_timeout_s | int | platform | 30 | false | VM/容器启动超时（秒） |
| sandbox.browser_docker_image | string | platform | arkloop/sandbox-browser:dev | false | Docker browser tier 使用的 sandbox-agent 镜像 |
| sandbox.credit_base_fee | int | platform | 1 | false | 每次 sandbox 调用的固定积分扣减，覆盖冷启动/调度开销 |
| sandbox.credit_rate_per_second | number | platform | 0.5 | false | sandbox 每秒执行时长对应的积分费率 |
| sandbox.docker_image | string | platform | arkloop/sandbox-agent:latest | false | Docker 后端使用的 sandbox-agent 镜像 |
| sandbox.flush_debounce_ms | int | platform | 2000 | false | flush debounce 延迟（毫秒） |
| sandbox.flush_force_bytes_threshold | int | platform | 16777216 | false | 触发强制 flush 的累计字节阈值 |
| sandbox.flush_force_count_threshold | int | platform | 512 | false | 触发强制 flush 的 dirty 数量阈值 |
| sandbox.flush_max_dirty_age_ms | int | platform | 15000 | false | 触发强制 flush 的最大 dirty 年龄（毫秒） |
| sandbox.idle_timeout_browser_s | int | platform | 120 | false | Sandbox browser tier 空闲超时（秒） |
| sandbox.idle_timeout_lite_s | int | platform | 180 | false | Sandbox lite tier 空闲超时（秒） |
| sandbox.idle_timeout_pro_s | int | platform | 300 | false | Sandbox pro tier 空闲超时（秒） |
| sandbox.max_lifetime_browser_s | int | platform | 600 | false | Sandbox browser tier 最大存活时间（秒） |
| sandbox.max_lifetime_s | int | platform | 1800 | false | Sandbox session 最大存活时间（秒） |
| sandbox.max_sessions | int | platform | 50 | false | Sandbox 最大并发 session 数 |
| sandbox.provider | string | platform | docker | false | Sandbox 后端类型：docker（自部署）/ vz（desktop 内嵌） |
| sandbox.refill_concurrency | int | platform | 2 | false | 预热补充最大并发数 |
| sandbox.refill_interval_s | int | platform | 5 | false | 预热补充检查间隔（秒） |
| sandbox.restore_ttl_days | int | platform | 7 | false | session restore state 保留天数 |
| sandbox.warm_browser | int | platform | 1 | false | browser tier 预热实例数 |
| sandbox.warm_lite | int | platform | 3 | false | lite tier 预热实例数 |
| sandbox.warm_pro | int | platform | 2 | false | pro tier 预热实例数 |
| skills.market.skillsmp_api_key | string | platform |  | true | SkillsMP 官方市场 API Key |
| skills.market.skillsmp_base_url | string | platform | https://skillsmp.com | false | SkillsMP 官方市场基础地址 |
| skills.registry.api_base_url | string | platform |  | false | 官方技能 Registry API 基础地址，留空则沿用 Base URL |
| skills.registry.api_key | string | platform |  | true | 官方技能 Registry API Key |
| skills.registry.base_url | string | platform | https://clawhub.ai | false | 官方技能 Registry 页面基础地址 |
| skills.registry.provider | string | platform | clawhub | false | 官方技能 Registry Provider |
| spawn.profile.explore | string | both | anthropic^claude-haiku-3-5 | false | Sub-agent 'explore' profile: 低延迟低成本模型 |
| spawn.profile.strong | string | both | anthropic^claude-sonnet-4-5 | false | Sub-agent 'strong' profile: 最强推理能力模型 |
| spawn.profile.task | string | both | anthropic^claude-sonnet-4-5 | false | Sub-agent 'task' profile: 平衡性价比模型 |
| spawn.profile.tool | string | both |  | false | 工具模型（标题摘要、结果摘要等子任务），格式 provider^model |
| spawn.profile.vision | string | both |  | false | 图像理解模型；persona.image_model 未设置时使用，格式 provider^model |
| suggestion.score_threshold | int | both | 15 | false | suggestion 更新触发阈值 |
| turnstile.allowed_host | string | platform |  | false | Turnstile Allowed Host |
| turnstile.secret_key | string | platform |  | true | Turnstile Secret Key |
| turnstile.site_key | string | platform |  | false | Turnstile Site Key |

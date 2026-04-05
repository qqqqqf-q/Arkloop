---
title: "Configuration Reference"
---

| key | type | scope | default | sensitive | description |
| --- | --- | --- | --- | --- | --- |
| credit.deduction_policy | string | platform | {"tiers":[{"up_to_tokens":2000,"multiplier":0},{"multiplier":1}]} | false | Credit deduction policy (JSON) |
| credit.initial_grant | int | platform | 1000 | false | Initial credit grant for new organizations |
| credit.invite_reward | int | platform | 500 | false | Reward credits for the inviter |
| credit.invitee_reward | int | platform | 200 | false | Reward credits for the invitee |
| credit.per_usd | int | platform | 1000 | false | Credit exchange rate: credits per 1 USD |
| email.from | string | platform |  | false | SMTP sender address; leave empty to disable email sending |
| email.smtp_host | string | platform |  | false | SMTP Host |
| email.smtp_pass | string | platform |  | true | SMTP Password |
| email.smtp_port | int | platform | 587 | false | SMTP Port |
| email.smtp_tls_mode | string | platform | starttls | false | SMTP TLS mode: starttls/tls/none |
| email.smtp_user | string | platform |  | false | SMTP Username |
| feature.byok_enabled | bool | both | true | false | Whether to allow org-level credentials (BYOK) |
| feature.mcp_remote_enabled | bool | both | false | false | Whether to allow remote MCP |
| gateway.ip_mode | string | platform | direct | false | Gateway IP mode: direct/cloudflare/trusted_proxy |
| gateway.ratelimit_capacity | number | platform | 600 | false | Gateway Rate Limit Capacity |
| gateway.ratelimit_rate_per_minute | number | platform | 300 | false | Gateway Rate Limit Per Minute |
| gateway.risk_reject_threshold | int | platform | 0 | false | Gateway risk rejection threshold (0-100) |
| gateway.trusted_cidrs | string | platform |  | false | List of Gateway trusted proxy CIDRs |
| invite.default_max_uses | int | both | 0 | false | Default maximum uses for an invitation code; 0 means unlimited |
| invite.max_codes_per_user | int | both | 1 | false | Maximum invitation codes a single user can create |
| limit.agent_reasoning_iterations | int | both | 0 | false | Maximum reasoning-turn limit for Agent Loop; 0 means unlimited |
| limit.tool_continuation_budget | int | both | 32 | false | Maximum continuation budget for long-running tools |
| limit.concurrent_runs | int | both | 0 | false | Maximum concurrent run limit; 0 means unlimited |
| limit.max_input_content_bytes | int | both | 32768 | false | Maximum byte size for Run input content submission |
| limit.max_parallel_tasks | int | platform | 32 | false | Maximum limit for Lua parallel tasks/parallel tool calls |
| limit.team_members | int | both | 0 | false | Maximum team member limit; 0 means unlimited |
| limit.thread_message_history | int | both | 200 | false | Maximum thread message history loading limit (count) |
| llm.max_response_bytes | int | platform | 16384 | false | Maximum limit for reading LLM Provider HTTP responses (bytes) |
| llm.retry.base_delay_ms | int | platform | 1000 | false | Base delay for LLM retries (milliseconds) |
| llm.retry.max_attempts | int | platform | 3 | false | Maximum number of LLM retry attempts |
| openviking.base_url | string | platform |  | false | OpenViking Base URL |
| openviking.cost_per_commit | number | platform | 0 | false | OpenViking CommitSession Cost (USD) |
| openviking.root_api_key | string | platform |  | true | OpenViking Root API Key |
| quota.runs_per_month | int | both | 0 | false | Monthly run quota; 0 means unlimited |
| quota.tokens_per_month | int | both | 0 | false | Monthly token quota; 0 means unlimited |
| sandbox.agent_port | int | platform | 8080 | false | Sandbox Agent listening port |
| sandbox.base_url | string | platform |  | false | Sandbox Service address; Worker calls Sandbox via this URL; if empty, sandbox tools are not registered |
| sandbox.boot_timeout_s | int | platform | 30 | false | VM/container boot timeout (seconds) |
| sandbox.credit_base_fee | int | platform | 1 | false | Fixed credit deduction per sandbox call to cover cold start/scheduling overhead |
| sandbox.credit_rate_per_second | number | platform | 0.5 | false | Credit rate per second of sandbox execution duration |
| sandbox.allow_egress | bool | platform | true | false | Whether Sandbox backends may access the public network |
| sandbox.docker_image | string | platform | arkloop/sandbox-agent:latest | false | sandbox-agent image used by the Docker backend |
| sandbox.idle_timeout_lite_s | int | platform | 180 | false | Idle timeout for Sandbox lite tier (seconds) |
| sandbox.idle_timeout_pro_s | int | platform | 300 | false | Idle timeout for Sandbox pro tier (seconds) |
| sandbox.idle_timeout_ultra_s | int | platform | 600 | false | Idle timeout for Sandbox ultra tier (seconds) |
| sandbox.max_lifetime_s | int | platform | 1800 | false | Maximum lifetime of a Sandbox session (seconds) |
| sandbox.max_sessions | int | platform | 50 | false | Maximum concurrent Sandbox sessions |
| sandbox.provider | string | platform | firecracker | false | Sandbox backend type: firecracker / docker |
| sandbox.refill_concurrency | int | platform | 2 | false | Maximum concurrency for pre-warm refill |
| sandbox.refill_interval_s | int | platform | 5 | false | Pre-warm refill check interval (seconds) |
| sandbox.warm_lite | int | platform | 3 | false | Number of pre-warmed instances for lite tier |
| sandbox.warm_pro | int | platform | 2 | false | Number of pre-warmed instances for pro tier |
| sandbox.warm_ultra | int | platform | 1 | false | Number of pre-warmed instances for ultra tier |
| turnstile.allowed_host | string | platform |  | false | Turnstile Allowed Host |
| turnstile.secret_key | string | platform |  | true | Turnstile Secret Key |
| turnstile.site_key | string | platform |  | false | Turnstile Site Key |
| web_fetch.firecrawl_api_key | string | both |  | true | Firecrawl API Key |
| web_fetch.firecrawl_base_url | string | both |  | false | Firecrawl Base URL |
| web_fetch.jina_api_key | string | both |  | true | Jina API Key |
| web_fetch.provider | string | both | basic | false | Web Fetch Provider: basic/firecrawl/jina |
| web_search.provider | string | both |  | false | Web Search Provider: searxng/tavily |
| web_search.searxng_base_url | string | both |  | false | SearXNG Base URL |
| web_search.tavily_api_key | string | both |  | true | Tavily API Key |

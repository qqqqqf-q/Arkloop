# Go Worker 变更记录（LLM 网关）

本文件用于记录本分支对 Go Worker LLM 网关的关键修复点，以及对应的人工验证方式。

## 1) OpenAI：支持 SSE streaming（chat_completions / responses）

- 现在会在请求里带上 `stream=true`，并在响应为 `text/event-stream` 时按 SSE 逐段解析。
- `chat_completions`：按 `choices[].delta.content` 连续产出 `message.delta`；并在 `finish_reason=tool_calls`/`[DONE]` 时汇总并产出 `tool_call`。
- `responses`：按 `*.delta` 事件产出 `message.delta`；在 `response.completed` 中解析 `output[].function_call` 并产出 `tool_call`。
- 超时策略从 `http.Client.Timeout` 切到 `context.WithTimeout`，避免长流式响应被客户端整体超时误杀。

人工验证（推荐）

1. 准备环境变量（示例，注意不要把 key 写进仓库文件）：
   - `export ARKLOOP_OPENAI_API_KEY=...`
   - `export ARKLOOP_TOOL_ALLOWLIST=echo`
   - `export ARKLOOP_LLM_DEBUG_EVENTS=1`（可选，用于把上游 chunk 记录进 run_events；**注意：该选项会把请求 payload 与响应内容写入数据库，仅限本地排障短期开启，切勿用于生产**）
   - `export ARKLOOP_PROVIDER_ROUTING_JSON='{"default_route_id":"r1","credentials":[{"id":"c1","scope":"platform","provider_kind":"openai","api_key_env":"ARKLOOP_OPENAI_API_KEY","openai_api_mode":"responses"}],"routes":[{"id":"r1","model":"gpt-4o-mini","credential_id":"c1"}]}'`
2. 启动 Postgres + API + Go Worker：
   - `docker compose up -d postgres`
   - `python -m alembic upgrade head`
   - `python -m uvicorn services.api.main:configure_app --factory --app-dir src --host 127.0.0.1 --port 8000`
   - `cd src/services/worker && go run ./cmd/worker`
3. 发起一次 run（CLI 或 curl 均可），让模型输出一段相对长的内容（便于观察多段 `message.delta`）：
   - `python -m arkloop chat --profile llm --message "请输出一段较长的文本，并在中途调用 echo 工具，参数 text=hello"`
4. 观察：
   - `run_events` 里应出现多条 `message.delta`（而不是只在最后出现一条）。
   - 若开启 `ARKLOOP_LLM_DEBUG_EVENTS=1`，应能看到多条 `llm.response.chunk`。

## 2) Anthropic：补齐 tool_use / tool_result 支持（非流式）

- 请求侧：把历史里的 `assistant.tool_calls` 映射为 `tool_use` block；把 `tool` role 的 envelope 映射为 `tool_result` block（按 Anthropic Messages API 约定）。
- 响应侧：解析 `content[]` 中的 `tool_use` block，并产出 `tool_call` 事件；不再假设 `content[0]` 必须是 text。

人工验证（推荐）

1. 准备环境变量：
   - `export ARKLOOP_ANTHROPIC_API_KEY=...`
   - `export ARKLOOP_TOOL_ALLOWLIST=echo`
   - `export ARKLOOP_PROVIDER_ROUTING_JSON='{"default_route_id":"r1","credentials":[{"id":"c1","scope":"platform","provider_kind":"anthropic","api_key_env":"ARKLOOP_ANTHROPIC_API_KEY"}],"routes":[{"id":"r1","model":"claude-3-5-sonnet-latest","credential_id":"c1"}]}'`
2. 同上启动 API + Go Worker。
3. 发起 run，并要求模型强制走一次工具调用：
   - `python -m arkloop chat --profile llm --message "请调用 echo 工具，参数 text=hello，然后把结果原样返回"`
4. 观察：
   - `run_events` 里应出现 `tool.call` / `tool.result`，且后续还能继续生成 `message.delta` 并终态完成。

## 3) OpenAI：错误信息与调试 chunk 的可观测性修复

- 非 2xx 时会尝试从响应 JSON 的 `error.message/type/code/param` 提取信息，写入 `run.failed.details`，避免只有“请求失败”。
- debug chunk 的 `truncated` 不再固定为 true：仅在实际截断（body 上限 / debug 上限）时才标记。

人工验证（推荐）

1. 临时设置一个无效的 OpenAI key：`export ARKLOOP_OPENAI_API_KEY=invalid`
2. 触发一次 run。
3. 观察：
   - `run.failed` 的 message/details 应包含上游返回的明确原因（例如 invalid_api_key）。

## 4) 小清理（P3）

- 删除不可达的 `api_mode 未实现` 分支，减少误导性的分支噪音。


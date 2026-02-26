# CLI 与环境 Profile（开发调试规划草案）

::: warning 已过时
此文档已过时，仅供历史参考。当前部署说明请参考 [部署指南](/guide/deployment)。
:::


## 0. 背景（为什么要做）

当前开发中有两个痛点：

1) 手工设置环境变量成本高：不同 provider、不同 base_url、不同 model、不同路由 JSON，靠手敲既慢又容易错。  
2) 缺少“可视化/可交互”的一键穿透入口：pytest 负责回归，但不擅长做演示与快速感知“后端现在到底能跑到哪”。

这份文档定义一个可持续的解决方案：用 CLI 作为“参考客户端 + 冒烟/排障工具”，并建立一套可复用的环境 Profile 机制。

## 1. 目标与非目标

### 1.1 目标

- 提供一个官方 CLI（参考客户端），能通过 HTTP 调用现有 API，并消费 SSE `run_events`（以事件流为唯一真相）。
- 支持“环境 Profile”：无需每次手敲 env；可以选择 `llm_test`、`dev`、`staging` 等 profile 运行。
- 安全默认：repo 内不存任何明文 secret；路由 JSON 中也不允许写入 secret（只允许引用 env 名）。
- 对前端有直接参考价值：CLI 的请求顺序、SSE 解析、断线续传（`after_seq`）可作为前端实现的黄金样例。

### 1.2 非目标（阶段性不做）

- 不把 CLI 当成测试框架替代 pytest；pytest 仍是验收/回归标准。
- 不在 Phase 1 做复杂的本地 UI（TUI/富文本）；先保证稳定、可脚本化（JSONL 输出）。
- 不在 Phase 1 引入“把 secret 存数据库/凭证管理”的能力（后续依赖 Console 的 P44/P43 等）。

## 2. 设计原则（与工程约定对齐）

- `run_events` 是唯一真相：CLI 展示/回放/拼接输出都以事件流为准。
- 断线重连用 `after_seq`：CLI 内部把 `after_seq` 当作唯一游标，并支持续传。
- 鉴权统一：HTTP 与 SSE 都走 `Authorization: Bearer ...`（Fetch 流式友好）。
- trace_id：默认由服务端生成；CLI 不伪造、也不信任用户随手输入的 trace_id。
- stdout 单行 JSON：CLI 默认输出 JSONL（每行一条），便于 grep、重放与问题定位。
- 跨平台：Windows/macOS/Linux 都能运行；避免依赖 shell 特性。

## 3. 推荐的仓库结构

> 目标：把“可运行的工具”变成工程的一部分，但不污染主业务代码与测试层。

- `src/apps/cli/`
  - CLI 应用入口（命令、参数解析、输出格式）
- `src/packages/arkloop_client/`（可选）
  - 纯 HTTP 客户端封装（登录、创建线程、发消息、创建 run、跟随事件等）
  - 供 CLI/未来脚本/甚至前端文档示例复用

说明：CLI 建议只通过 HTTP 调用 API（不要 import `services.api` 用 TestClient），这样它对前端才有同等参考价值。

## 4. 环境 Profile 方案（建议）

### 4.1 分层策略（强烈推荐）

1) 仓库可提交的“非敏感配置”
   - `provider routing` JSON 文件（不含 secret，只含 `api_key_env` 引用）
   - 示例：`src/apps/cli/config/routing.dev.json`
2) 每个开发者本机私有的“敏感配置”
   - API key、token、数据库密码等
   - 推荐放到用户目录（不进入 repo）

### 4.2 Profile 的落地方式

优先级从高到低：

- CLI 参数：`--dotenv-file ...`、`--routing-file ...`
- 显式环境变量：`ARKLOOP_DOTENV_FILE`、`ARKLOOP_PROVIDER_ROUTING_JSON`
- 仓库根目录 `.env`（仅在 `ARKLOOP_LOAD_DOTENV=1` 时启用）

推荐约定：

- 本机文件：`%USERPROFILE%\\.arkloop\\profiles\\llm_test.env`
- 可选的 repo 内本机文件（只在确有必要时使用）：
  - 放在 `src/apps/cli/env/.env.llm_test`（注意文件名以 `.env.` 开头，确保被 `.gitignore` 忽略）
  - 禁止使用 `*.env` 这种命名（容易误提交）

### 4.3 路由配置文件（避免手敲大 JSON）

路由 JSON 不应该包含明文 key，因此可以提交到仓库作为“可共享配置”：

- `credentials[].api_key_env`：只保存 env 名，例如 `ARKLOOP_OPENAI_API_KEY`
- `base_url/openai_api_mode/model`：可以写在 repo 的 routing 文件里，便于团队一致

CLI/脚本在运行时读取文件并注入到 `ARKLOOP_PROVIDER_ROUTING_JSON`，避免每次手动粘贴。

## 5. CLI 最小功能切片（建议路线）

### Milestone C1：单命令冒烟（最小可用）

命令示例（名称仅示意）：

- `arkloop chat --profile llm_test --message "hello"`

最小链路：

1) `POST /v1/auth/login`（或复用 token）
2) `POST /v1/threads`
3) `POST /v1/threads/{thread_id}/messages`
4) `POST /v1/threads/{thread_id}/runs`（可选 `route_id`）
5) `GET /v1/runs/{run_id}/events?after_seq=0&follow=1`
6) 依据 `message.delta` 聚合输出；遇到 `run.failed` 输出 `code + trace_id` 并非 0 退出

验收标准：

- 能在真实 provider（OpenAI/Anthropic/第三方网关）下跑通一次
- 输出包含 `thread_id/run_id/trace_id`，并且 JSONL 单行可被 grep

### Milestone C2：事件回放与续传

命令示例：

- `arkloop events follow --run-id ... --after-seq 123`

验收标准：

- after_seq 续传稳定；断线后可继续消费

### Milestone C3：Profile 与配置体验

命令示例：

- `arkloop profile list`
- `arkloop profile show llm_test`
- `arkloop chat --profile llm_test ...`

验收标准：

- 不需要手敲任何 env；只靠 profile 文件即可运行
- 任何 secret 不落入 repo；profile 文件路径可配置

## 6. 与前端协作方式（为什么它能推动前端）

- CLI 的请求与 SSE 解析逻辑可以直接转成前端实现清单：
  - 哪些请求先后顺序固定
  - `after_seq` 的续传策略
  - 如何展示 `run.failed` 的 `code + trace_id`
- CLI 的 JSONL 输出可以作为“前端 mock 数据来源”，也可作为 Console 的时间线视图参考。

## 7. 安全与合规清单（必须遵守）

- 禁止把任何 API key/token 写入仓库文件（包括 `src/apps/cli/env/`）。
- repo 内的 routing/config 只能引用 env 名（`api_key_env`），不得出现 `sk-...` 之类明文。
- 如果必须落盘 token，只能落到用户目录，且默认不开启（必须显式开关 + 明确提示）。


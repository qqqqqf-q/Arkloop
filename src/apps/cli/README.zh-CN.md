# Arkloop CLI（参考客户端）

目标：提供一个可脚本化的 CLI，用真实 HTTP 调用现有 API，并以 SSE `run_events` 作为唯一真相进行展示与回放。

## 运行方式

本仓库当前未打包成可安装的 `arkloop` 可执行文件，开发调试推荐用模块方式运行：

- Windows（PowerShell）：
  - `$env:PYTHONPATH="src"; .\\.venv\\Scripts\\python -m arkloop --help`
- Linux/macOS（bash/zsh）：
  - `PYTHONPATH=src ./.venv/bin/python -m arkloop --help`

## 环境 Profile

Profile 文件推荐放在用户目录（不进入仓库）：

- `~/.arkloop/profiles/<name>.env`（例如 `~/.arkloop/profiles/llm_test.env`）

也支持 repo 内的 profile 文件（建议只提交不含敏感信息的示例）：

- `src/apps/cli/env/.env.<name>`（例如 `src/apps/cli/env/.env.llm_test`）

仓库内已内置两个不含敏感信息的 profile，便于在 Python / Go API 之间切换：
- `api_py`：`http://127.0.0.1:8000`
- `api_go`：`http://127.0.0.1:8001`

如果你要新增包含明文 secret 的 profile，请放到用户目录，或确保 repo 内文件被 gitignore 忽略。

Profile 名称规则：
- 用户目录：`<name>.env` → `--profile <name>`（例如 `llm.env` 对应 `--profile llm`）
- repo 内：`.env.<name>` → `--profile <name>`

相关命令：

（在仓库根目录执行，并确保 `PYTHONPATH=src` 已设置）
- `python -m arkloop profile list`
- `python -m arkloop profile show <name>`
- `python -m arkloop chat --profile <name> --message "hello"`

说明：`profile show` 默认只输出 key 列表；如需显示明文值请加 `--reveal-values`。

## Provider Routing

可以用 `--routing-file` 把 routing JSON 文件内容注入到当前进程的 `ARKLOOP_PROVIDER_ROUTING_JSON`，避免手动粘贴长 JSON。

仓库内提供了一个不含任何明文 secret 的示例文件：

- `src/apps/cli/config/routing.dev.json`

注意：CLI 是独立进程的 HTTP 客户端，routing 配置真正生效在 API 进程；如果你是单独启动的 `uvicorn`，需要让 API 进程也加载同一份 routing 配置。

## 命令（Phase 1）

- `chat`：最小链路（login → threads → messages → runs → follow events）
  - `python -m arkloop chat --profile <name> --message "hello"`
  - 事件输出为 JSONL；遇到 `run.failed` 会输出 `code + trace_id` 并以非 0 退出。
- `events follow`：事件回放与续传（`after_seq` 作为唯一游标）
  - `python -m arkloop events follow --profile <name> --run-id <run_id> --after-seq 123`

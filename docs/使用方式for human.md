后端（Go API + Go Worker）

准备环境：
- 启动 venv（示例：`source ./.venv/bin/activate`）
- `export ARKLOOP_LOAD_DOTENV=1`
- `export ARKLOOP_DOTENV_FILE=".env.test"`（或 `.env`）
- `docker compose up -d postgres`
cd src/services/api
go run ./cmd/migrate up

启动 API（默认 8001；可用 `ARKLOOP_API_GO_ADDR` 或 `PORT` 覆盖）：
`cd src/services/api && go run ./cmd/api`

Worker（Go，另开终端）：
`cd src/services/worker && go run ./cmd/worker`

前端（本地开发默认走 Vite dev proxy，不直连 API，避免 CORS）：
- `pnpm -C src/apps/web dev`
  - 如需覆盖代理目标：`export ARKLOOP_API_PROXY_TARGET=http://127.0.0.1:8001`

CLI：
- `PYTHONPATH=src python -m arkloop chat --profile api --message "你好你是谁"`

测试：
- `pytest`
- `pytest -m integration`
- `cd src/services/worker && go test ./...`
  - integration 优先使用 `.env.test`，不要配置真实大模型

调试（仅本地短期开启）：
`ARKLOOP_LLM_DEBUG_EVENTS=1`

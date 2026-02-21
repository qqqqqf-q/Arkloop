后端（Go API + Go Worker）

准备环境：
- 启动 venv（示例：source ./.venv/bin/activate）
- export ARKLOOP_LOAD_DOTENV=1
- export ARKLOOP_DOTENV_FILE=".env.test"（或 .env）
- docker compose up -d postgres
cd src/services/api
go run ./cmd/migrate up

# API
cd src/services/api && go run ./cmd/api

# Worker
cd src/services/worker && go run ./cmd/worker

# 前端
pnpm -C src/apps/web dev
 如需覆盖代理目标：export ARKLOOP_API_PROXY_TARGET=http://127.0.0.1:8001

# Console前端
pnpm -C src/apps/console dev

调试（仅本地短期开启）：
ARKLOOP_LLM_DEBUG_EVENTS=1

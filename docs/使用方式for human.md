后端（Python API / Go API 可并行）

准备环境：
- 启动 venv（示例：`source ./.venv/bin/activate`）
- `export ARKLOOP_LOAD_DOTENV=1`
- `export ARKLOOP_DOTENV_FILE=".env.test"`（或 `.env`）
- `docker compose up -d postgres`
- `python -m alembic upgrade head`

启动 Python API（默认 8000）：
`python -m uvicorn services.api.main:configure_app --factory --app-dir src --host 127.0.0.1 --port 8000`

启动 Go API（默认 8001；可用 `ARKLOOP_API_GO_ADDR` 或 `PORT` 覆盖）：
`cd src/services/api_go && go run ./cmd/api`

Worker（Go，另开终端）：
`cd src/services/worker && go run ./cmd/worker`

前端（本地开发默认走 Vite dev proxy，不直连 API，避免 CORS）：
- 默认走 Python API：`pnpm -C src/apps/web dev`
- 切到 Go API（P.09 客户端切流）：`pnpm -C src/apps/web dev -- --mode api_go`
  - 也可显式设置：`export ARKLOOP_API_PROXY_TARGET=http://127.0.0.1:8001`

CLI（P.09 客户端切流；同一套协议切换 base_url）：
- Python API：`PYTHONPATH=src python -m arkloop chat --profile api_py --message "你好你是谁"`
- Go API：`PYTHONPATH=src python -m arkloop chat --profile api_go --message "你好你是谁"`

测试：
- `pytest`
- `pytest -m integration`
- `cd src/services/worker && go test ./...`
  - integration 优先使用 `.env.test`，不要配置真实大模型

调试（仅本地短期开启）：
`ARKLOOP_LLM_DEBUG_EVENTS=1`

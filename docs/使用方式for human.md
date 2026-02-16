后端
.\.venv\bin\activate
export ARKLOOP_LOAD_DOTENV=1
export ARKLOOP_DOTENV_FILE=".env.test"
docker compose up -d postgres
python -m alembic upgrade head
python -m uvicorn services.api.main:configure_app --factory --app-dir src --host 127.0.0.1 --port 8000

# Worker（另开一个终端，保持同样的 .env 配置）
PYTHONPATH=src python -m services.worker.main

# Worker（桥接模式：Go 消费 + Python 执行）
# 1) 启动 bridge（另开一个终端）
export ARKLOOP_WORKER_BRIDGE_TOKEN=please_change_me
python -m uvicorn services.worker_bridge.main:configure_app --factory --app-dir src --host 127.0.0.1 --port 18080

# 2) 启动 Go Worker（另开一个终端，保持同样的 .env 配置）
export ARKLOOP_WORKER_BRIDGE_URL=http://127.0.0.1:18080
export ARKLOOP_WORKER_BRIDGE_TOKEN=please_change_me
cd /Users/qqqqqf/Documents/Arkloop/src/services/worker_go
go run ./cmd/worker

# Go Worker 全量接管（桥接模式）
# 说明：需要在启动 API 前设置 ARKLOOP_WORKER_GO_TRAFFIC_PERCENT=100，让 enqueue 统一投递 go_bridge job_type。
# - API: export ARKLOOP_WORKER_GO_TRAFFIC_PERCENT=100
# - Go Worker: export ARKLOOP_WORKER_QUEUE_JOB_TYPES=run.execute.go_bridge
# - Python Worker: 停用（保留冷备）
#
# 回滚（秒级）
# - API: export ARKLOOP_WORKER_GO_TRAFFIC_PERCENT=0
# - Python Worker（冷备启动）：export ARKLOOP_WORKER_QUEUE_JOB_TYPES=run.execute,run.execute.go_bridge
# - 停用 Go Worker（bridge 可同时停用）

# 前端
pnpm -C src/apps/web dev

# 前端Debug(好像前后端都要运行一次吧)
export ARKLOOP_LLM_DEBUG_EVENTS=1 


测试
pytest
pytest -m integration

# integration 优先使用.env.test,请不要配置真实大模型,integration会报错

#llm发消息
python -m arkloop chat --profile llm --message "你好你是谁"

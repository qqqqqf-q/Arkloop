后端
.\.venv\bin\activate
export ARKLOOP_LOAD_DOTENV=1
export ARKLOOP_DOTENV_FILE=".env.test"
docker compose up -d postgres
python -m alembic upgrade head
python -m uvicorn services.api.main:configure_app --factory --app-dir src --host 127.0.0.1 --port 8000

# Worker（另开一个终端，保持同样的 .env 配置）
PYTHONPATH=src python -m services.worker.main

cd /Users/qqqqqf/Documents/Arkloop/src/services/worker_go
go run ./cmd/worker

# 前端
pnpm -C src/apps/web dev

# 前端Debug(好像前后端都要运行一次吧)
ARKLOOP_LLM_DEBUG_EVENTS=1

测试
pytest
pytest -m integration
cd src/services/worker && go test ./...
# integration 优先使用.env.test,请不要配置真实大模型,integration会报错

#llm发消息
python -m arkloop chat --profile llm --message "你好你是谁"

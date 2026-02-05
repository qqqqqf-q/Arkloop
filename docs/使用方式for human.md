后端
.\.venv\Scripts\activate.ps1
$env:ARKLOOP_LOAD_DOTENV=1
docker compose up -d postgres
python -m alembic upgrade head
python -m uvicorn services.api.main:configure_app --factory --app-dir src --host 127.0.0.1 --port 8000

前端
pnpm -C src/apps/web dev

测试
pytest
pytest -m integration

#llm发消息
python -m arkloop chat --profile llm --message "你好你是谁"

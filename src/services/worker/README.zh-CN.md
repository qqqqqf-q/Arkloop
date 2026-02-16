# worker_go（WG01）

`worker_go` 是 Go 版 Worker 的最小骨架，当前仅负责：
- 解析 Worker 环境变量配置
- 输出 JSON 日志（包含 `trace_id/org_id/run_id/job_id`）
- 消费 `jobs` 队列（可选）

执行模式：
- 未设置 `ARKLOOP_DATABASE_URL` / `DATABASE_URL`：只启动并等待退出（用于快速验证二进制与配置）。
- 设置了数据库连接串：进入消费模式，默认使用 noop handler（会 ack job，但不执行引擎）。
- 设置 `ARKLOOP_WORKER_BRIDGE_URL`：进入桥接模式，把 `payload_json` 转发给 Python bridge 执行。

## 目录结构

```text
src/services/worker_go/
  cmd/worker/main.go
  internal/app/
```

## 环境变量

- `ARKLOOP_WORKER_CONCURRENCY`（默认 4）
- `ARKLOOP_WORKER_POLL_SECONDS`（默认 0.25）
- `ARKLOOP_WORKER_LEASE_SECONDS`（默认 30）
- `ARKLOOP_WORKER_HEARTBEAT_SECONDS`（默认 10）
- `ARKLOOP_WORKER_QUEUE_JOB_TYPES`：消费的 `jobs.job_type` 列表（逗号分隔，默认 `run.execute`）
- `ARKLOOP_DATABASE_URL` / `DATABASE_URL`：Postgres 连接串（设置后进入消费模式）
- `ARKLOOP_WORKER_BRIDGE_URL`：Python bridge base url（例如 `http://127.0.0.1:18080`）
- `ARKLOOP_WORKER_BRIDGE_TOKEN`：bridge 的 shared token（bridge 模式必填）

## 本地测试

```bash
cd /Users/qqqqqf/Documents/Arkloop/src/services/worker_go
go test ./...
```

## 多平台构建

```bash
cd /Users/qqqqqf/Documents/Arkloop/src/services/worker_go
GOOS=linux GOARCH=amd64 go build ./cmd/worker
GOOS=darwin GOARCH=arm64 go build ./cmd/worker
GOOS=windows GOARCH=amd64 go build ./cmd/worker
```

## 本地桥接运行（WG04）

先启动 Python bridge：

```bash
cd /Users/qqqqqf/Documents/Arkloop
export ARKLOOP_LOAD_DOTENV=1
export ARKLOOP_WORKER_BRIDGE_TOKEN=please_change_me
python -m uvicorn services.worker_bridge.main:configure_app --factory --app-dir src --host 127.0.0.1 --port 18080
```

再启动 Go Worker（桥接模式）：

```bash
cd /Users/qqqqqf/Documents/Arkloop/src/services/worker_go
export ARKLOOP_WORKER_BRIDGE_URL=http://127.0.0.1:18080
export ARKLOOP_WORKER_BRIDGE_TOKEN=please_change_me
go run ./cmd/worker
```

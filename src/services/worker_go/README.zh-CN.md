# worker_go（WG01）

`worker_go` 是 Go 版 Worker 的最小骨架，当前仅负责：
- 解析 Worker 环境变量配置
- 输出 JSON 日志（包含 `trace_id/org_id/run_id/job_id`）
- 启动后等待退出信号（默认不消费任务）

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

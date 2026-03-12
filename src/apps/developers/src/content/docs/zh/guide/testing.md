---
---

# 测试与基准

## 日常 CI 检查

日常本地验证建议先使用仓库内的 CI 辅助脚本：

```bash
# 快速本地检查
bin/ci-local quick

# 启动临时 PostgreSQL 后跑 Go 集成测试
bin/ci-local integration

# 完整本地检查
bin/ci-local full

# 近似 GitHub Actions 的验证
bin/ci-local act go-check
bin/ci-local act typescript
```

推荐顺序：`bin/ci-local quick` -> `bin/ci-local integration` -> `bin/ci-local act <job>`。
`quick` 适合日常提交前自检，`integration` 适合数据库、repo、worker pipeline、webhook、runengine 一类改动，`act` 用来做接近 GitHub Actions 的补充验证。
`quick` 会自动安装前端依赖，因此首次运行会更慢。
`bin/ci-local act ...` 首次执行会拉取体积较大的 runner 镜像。
当前不建议使用 `bin/ci-local act go-integration`，本地集成检查优先使用 `bin/ci-local integration`。

## 单元测试

```bash
# Go
cd src/services/api && go test ./...
cd src/services/worker && go test ./...
cd src/services/gateway && go test ./...

# 前端
cd src/apps/web && pnpm test
cd src/apps/console && pnpm test
```

## 集成测试

```bash
bin/ci-local integration
```

如果要单独深挖某个服务，可进入对应目录后执行 `go test -count=1 -race ./...`。

## 冒烟测试

冒烟测试对运行中的 Compose 环境做端到端验证（健康检查、注册、登录、创建线程、发送消息、SSE 流式传输）。

```bash
docker compose up -d

ARKLOOP_SMOKE_API_URL=http://127.0.0.1:19000 \
  go test -tags smoke ./tests/smoke/...
```

## 基准测试（Baseline）

基准测试套件测量单节点下核心服务（Gateway、API、Worker + stub LLM）的吞吐量与延迟。

### 前置条件

启动专用的 bench Compose 环境（端口偏移 +5 以避免冲突）：

```bash
docker compose -f compose.bench.yaml -p arkloop-bench up -d
```

默认 bench 端口：

| 服务 | 端口 |
|------|------|
| Gateway | `http://127.0.0.1:8005` |
| API | `http://127.0.0.1:8006` |
| Postgres | `127.0.0.1:5437` |

设置 `DATABASE_URL` 以启用自动注册和 `pg_stat_activity` 采集：

```bash
export DATABASE_URL="postgresql://arkloop:<ARKLOOP_POSTGRES_PASSWORD>@127.0.0.1:5437/arkloop"
```

### 运行 Baseline

```bash
go run ./tests/bench/cmd/bench baseline \
  -out /tmp/arkloop-baseline.json
```

可选：包含 OpenViking 测试（需要运行中的实例和 root key）：

```bash
go run ./tests/bench/cmd/bench baseline \
  -include-openviking \
  -openviking-root-key "$ARKLOOP_OPENVIKING_ROOT_API_KEY" \
  -out /tmp/arkloop-baseline.json
```

### 结果解读

输出为 JSON 格式。`overall_pass=false` 时进程以退出码 1 结束。

| 字段 | 说明 |
|------|------|
| `results[].pass` | 单场景通过/失败 |
| `results[].stats.latency_ms` | 延迟分布 |
| `results[].stats.pg_stat_activity_max_*` | 测试期间数据库连接峰值 |
| `*.stats.net_error_kinds` | 网络错误分类（超时、拒绝、重置） |

### 推荐环境变量

`compose.bench.yaml` 已内置合理默认值。手动运行时的关键覆盖项：

```bash
# Gateway: 关闭限流
ARKLOOP_RATELIMIT_CAPACITY=120000
ARKLOOP_RATELIMIT_RATE_PER_MINUTE=120000

# API: 并发 Run 上限
ARKLOOP_LIMIT_CONCURRENT_RUNS=60

# Worker: 并行执行数
ARKLOOP_WORKER_CONCURRENCY=50
```

认证方式：显式传入 `-access-token`，或确保 `DATABASE_URL` 已设置以启用自动注册。

### 故障排查

| 错误 | 原因 |
|------|------|
| `gateway.not_ready` / `api.not_ready` | 服务未就绪，检查 `/healthz` |
| `gateway_ratelimit` 返回 404 | 未设置 `ARKLOOP_GATEWAY_ENABLE_BENCHZ`（bench compose 默认启用） |
| `auth.register.code.auth.invite_code_required` | 注册需要邀请码，使用 `-force-open-registration` 或传入 token |
| `worker_runs.runs_create_failed` 偏高 | `limit.concurrent_runs` 过低或 Worker 未消费队列 |

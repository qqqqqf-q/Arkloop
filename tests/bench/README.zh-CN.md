# G2 压力测试基线

本目录保存单节点压测脚本与一次基线结果。

假设与边界：

- 基线结果来自 docker compose 单机开发环境，不同机器差异很大；用于趋势参考，不作为跨环境硬门槛

## Prerequisites

启动专用 bench compose（端口为标准端口 +5，不影响你的日常开发 compose）：

```bash
docker compose -f compose.bench.yaml -p arkloop-bench up -d
```

说明：

- bench 默认端口：
  - Gateway：`http://127.0.0.1:8005`
  - API：`http://127.0.0.1:8006`
  - Postgres：`127.0.0.1:5437`（用于 bench 自动注册/bootstrapping）
- bench compose 内置独立的 `redis_gateway`（禁用持久化），仅供 Gateway 限流/鉴权链路使用，避免和主 Redis 的持久化抖动互相干扰
- baseline suite 只跑核心链路（Gateway / API / Worker + stub LLM），不包含 Sandbox 等外部能力压测

bench 自动注册依赖 `DATABASE_URL`（连到 bench 的 Postgres）：

```bash
export DATABASE_URL="postgresql://arkloop:<你的 ARKLOOP_POSTGRES_PASSWORD>@127.0.0.1:5437/arkloop"
```

说明：

- 只要设置了 `DATABASE_URL`（或显式传 `-db-dsn`），baseline 会自动采集 `pg_stat_activity` 的峰值（用于定位 DB 连接池/排队问题）

## Recommended Env

`compose.bench.yaml` 已内置基线推荐值（通常不需要你再额外设置）：

```bash
# Gateway：避免 429 干扰吞吐与延迟
ARKLOOP_RATELIMIT_CAPACITY=120000
ARKLOOP_RATELIMIT_RATE_PER_MINUTE=120000

# API：并发 run 上限（默认 10，不满足 worker_runs=50）
ARKLOOP_LIMIT_CONCURRENT_RUNS=60

# Worker：并发执行（默认 4，会导致 runs 排队超时）
ARKLOOP_WORKER_CONCURRENCY=50
```

认证方式（二选一）：

- 显式提供 token：`-access-token "$ARKLOOP_BENCH_ACCESS_TOKEN"`
- 自动注册：需要 `DATABASE_URL` 可用；并在必要时加 `-force-open-registration`

## Run Baseline

在仓库根目录执行：

```bash
go run ./tests/bench/cmd/bench baseline \
  -out docs/benchmark/baseline-2026-03-03.json
```

如果你不想提前 `export DATABASE_URL`，也可以用一条命令内联（推荐在 bench compose 下使用）：

```bash
DATABASE_URL="postgresql://arkloop:<你的 ARKLOOP_POSTGRES_PASSWORD>@127.0.0.1:5437/arkloop" \
  go run ./tests/bench/cmd/bench baseline -out docs/benchmark/baseline-2026-03-03.json
```

如果只是本地跑一次、不想改动仓库里的基线文件，建议输出到临时路径：

```bash
go run ./tests/bench/cmd/bench baseline -out /tmp/arkloop-baseline.json
```

Worker 场景默认使用当前仓库内存在的 `normal` persona；如果你要指定其他 persona，可显式传：

```bash
go run ./tests/bench/cmd/bench baseline \
  -worker-persona normal \
  -out /tmp/arkloop-baseline.json
```

## Interpretation

- 输出为 JSON，`overall_pass=false` 时进程退出码为 1
- `results[].pass` 是单项结论
- `results[].stats` 包含指标与采样结果（例如 `latency_ms`、`retention`、`run_completion_ms`）
- `gateway_ratelimit` 默认请求 Gateway 的 `/benchz`（完整中间件链路）；`/healthz` 仅用于存活探针
- `api_crud.stats.pg_stat_activity_max_total` / `api_crud.stats.pg_stat_activity_max_active`：压测期间 DB 连接数峰值（需要 `DATABASE_URL` 或 `-db-dsn`）
- `worker_runs.stats.pg_stat_activity_max_total` / `worker_runs.stats.pg_stat_activity_max_active`：同上
- `*.stats.net_error_kinds`：网络错误类型聚合（便于区分超时/拒绝/重置等）

## Troubleshooting

- `gateway.not_ready` / `api.not_ready`：服务未就绪（检查对应服务 `/healthz`）
- `gateway_ratelimit` 返回 404：确认 Gateway 已启用 `/benchz`（bench compose 默认设置 `ARKLOOP_GATEWAY_ENABLE_BENCHZ=true`），并可直接 `curl http://127.0.0.1:8005/benchz` 验证
- `auth.register.code.auth.invite_code_required`：注册模式为 invite_only（需要配置 `registration.open=true` 或提供邀请码/显式 token）
- `worker_runs.runs_create_failed` 很高：通常是 `limit.concurrent_runs` 太低或 Worker 未消费队列
- `worker_runs` 出现 `persona.not_found`：确认 bench 使用的 persona 仍存在；默认是 `normal`，也可显式传 `-worker-persona <persona_id>`

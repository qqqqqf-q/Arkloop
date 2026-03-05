# Testing & Benchmarks

## Unit Tests

```bash
# Go
cd src/services/api && go test ./...
cd src/services/worker && go test ./...
cd src/services/gateway && go test ./...

# Frontend
cd src/apps/web && pnpm test
cd src/apps/console && pnpm test
```

## Integration Tests

```bash
cd src/services/api && go test -tags integration ./...
```

## Smoke Tests

Smoke tests verify a running Compose stack end-to-end (health check, register, login, create thread, send message, SSE streaming).

```bash
docker compose up -d

ARKLOOP_SMOKE_API_URL=http://127.0.0.1:8000 \
  go test -tags smoke ./tests/smoke/...
```

## Benchmark (Baseline)

Benchmark suite measures single-node throughput and latency across core services (Gateway, API, Worker + stub LLM).

### Prerequisites

Start the dedicated bench Compose stack (ports offset by +5 to avoid conflicts):

```bash
docker compose -f compose.bench.yaml -p arkloop-bench up -d
```

Default bench ports:

| Service | Port |
|---------|------|
| Gateway | `http://127.0.0.1:8005` |
| API | `http://127.0.0.1:8006` |
| Browser | `http://127.0.0.1:3105` (optional, `--profile tools`) |
| Postgres | `127.0.0.1:5437` |

Set `DATABASE_URL` for auto-registration and `pg_stat_activity` collection:

```bash
export DATABASE_URL="postgresql://arkloop:<ARKLOOP_POSTGRES_PASSWORD>@127.0.0.1:5437/arkloop"
```

### Run Baseline

```bash
go run ./tests/bench/cmd/bench baseline \
  -out /tmp/arkloop-baseline.json
```

Optional: include OpenViking (requires a running instance and root key):

```bash
go run ./tests/bench/cmd/bench baseline \
  -include-openviking \
  -openviking-root-key "$ARKLOOP_OPENVIKING_ROOT_API_KEY" \
  -out /tmp/arkloop-baseline.json
```

Browser-only benchmark:

```bash
docker compose -f compose.bench.yaml -p arkloop-bench --profile tools up -d

go run ./tests/bench/cmd/bench browser \
  -out /tmp/arkloop-browser.json
```

### Interpreting Results

Output is JSON. `overall_pass=false` exits with code 1.

| Field | Description |
|-------|-------------|
| `results[].pass` | Per-scenario pass/fail |
| `results[].stats.latency_ms` | Latency distribution |
| `results[].stats.pg_stat_activity_max_*` | DB connection peak during test |
| `*.stats.net_error_kinds` | Network error breakdown (timeout, refused, reset) |

### Recommended Env

`compose.bench.yaml` ships with sensible defaults. Key overrides if running manually:

```bash
# Gateway: disable rate limiting
ARKLOOP_RATELIMIT_CAPACITY=120000
ARKLOOP_RATELIMIT_RATE_PER_MINUTE=120000

# API: concurrent run cap
ARKLOOP_LIMIT_CONCURRENT_RUNS=60

# Worker: parallel execution
ARKLOOP_WORKER_CONCURRENCY=50
```

Authentication: either provide `-access-token` explicitly, or ensure `DATABASE_URL` is set for auto-registration.

### Troubleshooting

| Error | Cause |
|-------|-------|
| `gateway.not_ready` / `api.not_ready` | Service not healthy, check `/healthz` |
| `gateway_ratelimit` returns 404 | `ARKLOOP_GATEWAY_ENABLE_BENCHZ` not set (bench compose enables it by default) |
| `browser.not_ready` | Browser service not started, enable `--profile tools` |
| `auth.register.code.auth.invite_code_required` | Registration is invite-only, use `-force-open-registration` or provide a token |
| `worker_runs.runs_create_failed` high | `limit.concurrent_runs` too low or Worker not consuming queue |

# Local Setup

## Dependencies

- Go 1.22+
- Node.js 20+
- pnpm
- Docker (for running PostgreSQL)

## 1. Start PostgreSQL

```bash
cp .env.example .env
# Edit .env, set ARKLOOP_POSTGRES_PASSWORD
docker compose up -d
```

Connectivity verification:

```bash
docker compose exec -T postgres psql -U arkloop -d arkloop -c "select 1;"
```

## 2. Start API Service

Default listener is `127.0.0.1:8001`.

::: code-group

```bash [Linux/macOS]
export ARKLOOP_LOAD_DOTENV=1
export ARKLOOP_DOTENV_FILE=.env
cd src/services/api && go run ./cmd/api
```

```powershell [Windows]
$env:ARKLOOP_LOAD_DOTENV="1"
$env:ARKLOOP_DOTENV_FILE=".env"
cd src/services/api; go run ./cmd/api
```

:::

Override listener address:

```
ARKLOOP_API_GO_ADDR=127.0.0.1:8001
```

## 3. Start Worker Service

```bash
cd src/services/worker && go run ./cmd/worker
```

## 4. Start Frontend (Web)

`docker compose up -d` already includes Gateway (default listening on 8000), frontend proxy should point to Gateway:

::: code-group

```bash [Linux/macOS]
export ARKLOOP_API_PROXY_TARGET=http://127.0.0.1:8000
cd src/apps/web && pnpm install && pnpm dev
```

```powershell [Windows]
$env:ARKLOOP_API_PROXY_TARGET="http://127.0.0.1:8000"
cd src/apps/web; pnpm install; pnpm dev
```

:::

## 5. Start Frontend (Console)

::: code-group

```bash [Linux/macOS]
export ARKLOOP_API_PROXY_TARGET=http://127.0.0.1:8000
cd src/apps/console && pnpm install && pnpm dev
```

```powershell [Windows]
$env:ARKLOOP_API_PROXY_TARGET="http://127.0.0.1:8000"
cd src/apps/console; pnpm install; pnpm dev
```

:::

## Integration Testing

```bash
cd src/services/api && go test -tags integration ./...
```

## Environment Variables Quick Reference

| Variable | Description | Default |
|------|------|--------|
| `ARKLOOP_DATABASE_URL` | PostgreSQL connection string | — |
| `ARKLOOP_API_GO_ADDR` | API listener address | `127.0.0.1:8001` |
| `ARKLOOP_LOAD_DOTENV` | Load from .env file automatically | `0` |
| `ARKLOOP_DOTENV_FILE` | .env file path | `.env` |
| `ARKLOOP_TOOL_ALLOWLIST` | Allowed built-in tools for LLM (comma-separated) | Empty (all disabled) |
| `ARKLOOP_JWT_SECRET` | JWT signing secret | — |

Full environment variable reference: `.env.example`.

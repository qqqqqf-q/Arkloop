# Open Source Boundary

This document defines the open-source boundary of the Arkloop repository: what belongs to the OSS core, what are configuration templates, and what must not appear in the public repository.

## Directory Classification

### OSS Core (fully public)

| Path | Description |
|------|-------------|
| `src/services/api/` | API service |
| `src/services/gateway/` | Gateway reverse proxy |
| `src/services/worker/` | Worker execution engine |
| `src/services/sandbox/` | Sandbox service |
| `src/services/browser/` | Browser automation service |
| `src/services/shared/` | Shared Go libraries |
| `src/apps/web/` | Web frontend (brand protection per LICENSE) |
| `src/apps/console/` | Console admin dashboard (brand protection per LICENSE) |
| `src/apps/cli/` | CLI reference client |
| `src/apps/shared/` | Shared frontend packages |
| `src/personas/` | Persona templates |
| `src/docs/` | Technical documentation (VitePress) |
| `tests/` | Tests (including benchmarks) |
| `config/sandbox/templates.json` | Sandbox template definitions |
| `config/openviking/ov.conf.example` | OpenViking configuration template |
| `compose.yaml` | Docker Compose orchestration |
| `compose.bench.yaml` | Benchmark orchestration |
| `.github/workflows/` | CI pipelines |
| `README.md` | Project description |
| `CONTRIBUTING.md` | Contribution guidelines |
| `CODE_OF_CONDUCT.md` | Code of conduct |
| `SECURITY.md` | Security disclosure policy |

### Configuration Templates (public, no real values)

| Path | Description |
|------|-------------|
| `.env.example` | Environment variable template (all values are placeholders, includes test overrides) |
| `config/openviking/ov.conf.example` | OpenViking configuration template |

### Excluded (via .gitignore or pre-release cleanup)

| Path | Reason | Action |
|------|--------|--------|
| `.env` / `.env.*` (non-example) | Real secrets | .gitignore |
| `config/openviking/ov.conf` | Real API Key | .gitignore |
| `.claude` / `CLAUDE.md` | AI IDE private config | .gitignore |
| `review.md` | AI review spec (internal toolchain) | .gitignore |
| `temp/` | Temporary files | .gitignore |
| `.VSCodeCounter/` | Code stats cache | .gitignore |
| `src/docs/.vitepress/dist/` | Build artifacts | .gitignore |
| `node_modules/` | Dependencies | .gitignore |

## Pre-Release Cleanup Checklist

- [x] No real API Key / Token / password leaks in git history
- [x] All `.env` files are in `.gitignore`
- [x] `config/openviking/ov.conf` (contains root_api_key) is in `.gitignore`
- [x] No internal domain names or private registry URLs hardcoded
- [x] No personal local paths hardcoded (cleaned `/Users/qqqqqf/` references)
- [x] Documentation "internal" markers changed to public context
- [x] `.dockerignore` created to prevent `.env` / `.git/` leaking into builds
- [x] Trademark usage rules documented in CONTRIBUTING.md

## License Boundary

Primary license is the Arkloop License (modified Apache 2.0), with additional terms:

1. **Multi-tenant restriction**: Source code may not be used to operate a multi-tenant SaaS (one Organization = one tenant)
2. **Brand protection**: LOGO and copyright information in `src/apps/web/` and `src/apps/console/` must not be removed

See the root `LICENSE` file for full details.

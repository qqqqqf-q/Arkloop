# Contributing to Arkloop

Thank you for considering a contribution to Arkloop. This document covers the process and guidelines for contributing.

## Getting Started

### Prerequisites

- Go 1.26+
- Node.js 20+ with pnpm
- Docker and Docker Compose
- PostgreSQL 16+ (or use `docker compose up postgres`)

### Local Development Setup

```bash
git clone https://github.com/qqqqqf/Arkloop.git
cd Arkloop

# Start infrastructure
docker compose up -d postgres redis seaweedfs pgbouncer

# Copy and configure environment
cp .env.example .env
# Edit .env with your local configuration

# Backend (Go services)
cd src/services/api && go run . &
cd src/services/gateway && go run . &
cd src/services/worker && go run .

# Frontend
cd src/apps/web && pnpm install && pnpm dev
cd src/apps/console && pnpm install && pnpm dev
```

### Project Structure

```
src/
  apps/
    web/          # User-facing chat interface (React)
    console/      # Admin dashboard (React)
    cli/          # CLI reference client
    shared/       # Shared frontend packages
  services/
    api/          # Core REST API (Go)
    gateway/      # Reverse proxy (Go)
    worker/       # Job execution engine (Go)
    sandbox/      # Code execution sandbox (Go)
    shared/       # Shared Go libraries
  personas/       # Agent persona templates
  docs/           # Documentation (VitePress)
```

## How to Contribute

### Reporting Bugs

Open an issue on [GitHub Issues](https://github.com/qqqqqf/Arkloop/issues) with:

- Steps to reproduce
- Expected vs. actual behavior
- Environment details (OS, Docker version, browser)

### Suggesting Features

Open a discussion or issue describing the use case and proposed solution. We prefer concrete problem descriptions over abstract feature requests.

### Submitting Code

1. Fork the repository and create a feature branch from `main`.
2. Make your changes following the code conventions below.
3. Write or update tests for your changes.
4. Run linting and tests to verify nothing is broken.
5. Submit a pull request with a clear description.

### Code Conventions

**Commits**

Format:

```
<type>(<scope>): <subject>

<body>

<footer>
```

- **Header** (required): `<type>(<scope>): <subject>`
  - `type`: one of the types below
  - `scope`: affected area (optional, e.g., `auth`, `parser`, `api`)
  - `subject`: short description, imperative mood, lowercase, no trailing period
  - Keep header under 50 characters

| Type | Description |
|------|-------------|
| **feat** | New feature |
| **fix** | Bug fix |
| **docs** | Documentation only |
| **style** | Formatting, no logic change |
| **refactor** | Neither fix nor feature |
| **perf** | Performance improvement |
| **test** | Add or correct tests |
| **build** | Build system or dependency changes |
| **ci** | CI configuration changes |
| **chore** | Other non-source changes |
| **revert** | Revert a previous commit |

Rules:

- No emoji in commit messages
- Atomic commits: one logical change per commit
- Use the primary project language (or follow recent git history language)
- No `Co-authored-by` or AI attribution trailers

Examples:

```
feat(parser): add support for nested json objects
```

```
fix(auth): correct token expiration logic

The previous logic used milliseconds instead of seconds, causing
tokens to expire prematurely in production environments.

Close #123
```

**Go**

- Follow standard Go conventions and project linting rules
- Keep functions short and focused
- Handle all errors explicitly

**TypeScript / React**

- Use TypeScript strict mode
- Follow the existing Tailwind CSS patterns
- Linting: the project uses ESLint and Prettier

**Python (Worker internals)**

- Follow Ruff rules defined in `pyproject.toml`

### Running Tests

```bash
# Go unit tests
cd src/services/api && go test ./...
cd src/services/worker && go test ./...
cd src/services/gateway && go test ./...

# Frontend tests
cd src/apps/web && pnpm test
cd src/apps/console && pnpm test

# Integration / smoke tests
cd tests/smoke && go test ./...
```

## Trademark Usage

The Arkloop name, logo, and brand assets are trademarks of The Arkloop Authors.

- You may use the Arkloop name to accurately describe your relationship with the project (e.g., "built on Arkloop", "compatible with Arkloop").
- You may not use the Arkloop name, logo, or brand assets in a way that implies official endorsement or affiliation without written permission.
- As stated in the [LICENSE](LICENSE), frontend components (`src/apps/web/` and `src/apps/console/`) must retain the original LOGO and copyright information.

## Contributor License

By submitting a contribution, you agree that:

1. The project maintainers may adjust the open-source license terms as described in the [LICENSE](LICENSE).
2. Your contributed code may be used for commercial purposes, including cloud operations.

These terms are detailed in Section 2 of the Arkloop License.

## Questions

If you have questions about contributing, open a discussion on GitHub or reach out to the maintainers.

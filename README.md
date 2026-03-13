<p align="center">
  <img src="https://img.shields.io/badge/Arkloop-AI%20Agent%20Platform-000000?style=for-the-badge&labelColor=000000" alt="Arkloop" />
</p>

<p align="center">
  <a href="https://arkloop.ai">Arkloop Cloud</a> &middot;
  <a href="#self-hosting">Self-hosting</a> &middot;
  <a href="https://docs.arkloop.ai">Documentation</a>
</p>

<p align="center">
  <a href="./LICENSE"><img alt="License" src="https://img.shields.io/badge/license-Arkloop%20License-blue"></a>
  <a href="https://github.com/qqqqqf/Arkloop/graphs/commit-activity"><img alt="Commits" src="https://img.shields.io/github/commit-activity/m/qqqqqf/Arkloop?labelColor=%2332b583&color=%2312b76a"></a>
  <a href="https://github.com/qqqqqf/Arkloop/issues"><img alt="Issues" src="https://img.shields.io/github/issues-search?query=repo%3Aqqqqqf%2FArkloop%20is%3Aclosed&label=issues%20closed&labelColor=%237d89b0&color=%235d6b98"></a>
  <a href="https://twitter.com/intent/follow?screen_name=qqqqqf_"><img alt="Follow on X" src="https://img.shields.io/twitter/follow/qqqqqf_?logo=X&color=%20%23f5f5f5"></a>
</p>

<p align="center">
  <a href="./README.md"><img alt="English" src="https://img.shields.io/badge/English-blue"></a>
  <a href="./docs/zh-CN/README.md"><img alt="简体中文" src="https://img.shields.io/badge/简体中文-d9d9d9"></a>
</p>

Arkloop is an open-source AI agent platform that fuses autonomous task execution, real-time intelligent search, and secure sandboxed workspace into a single product. It integrates Memory for emotional support. It brings together the capabilities of Manus-style agents, Perplexity-grade search synthesis, and cloud-native infrastructure.

## Quick Start

### Arkloop Cloud

The fastest way to experience Arkloop -- zero setup, fully managed.

[Get started on Arkloop Cloud](https://arkloop.ai)

### Self-hosting

> System requirements: Docker, Docker Compose, and Python 3 installed, 2+ CPU cores, 4+ GiB RAM.

```bash
git clone https://github.com/qqqqqf/Arkloop.git
cd Arkloop
./setup.sh install
```

For a non-interactive install, pass explicit parser flags to `./setup.sh install --non-interactive ...`.
If port `19000` is already in use, set another gateway port, for example `./setup.sh install --gateway-port 8100`.
If you want English prompts and logs, pass `--lang en`.
Once the install is healthy, access the Console entry at `http://localhost:19000` or your custom gateway port.

For host-level debugging ports such as PostgreSQL, API, Sandbox, or OpenViking, opt in explicitly:

```bash
docker compose -f compose.yaml -f compose.dev.yaml up -d
```

### Production Deployment

For production, use pre-built multi-arch images (amd64 + arm64) from `ghcr.io/qqqqqf-q/arkloop-*` instead of building locally:

```bash
# Pull pre-built images and start
docker compose -f compose.yaml -f compose.prod.yaml up -d

# Pin a specific version
ARKLOOP_VERSION=v0.5.0 docker compose -f compose.yaml -f compose.prod.yaml up -d

# Pull images first (useful for zero-downtime upgrades)
docker compose -f compose.yaml -f compose.prod.yaml pull
```

The `--prod` flag in `setup.sh` enables this automatically:

```bash
./setup.sh install --prod --non-interactive ...
```

For detailed configuration, environment variables, and production deployment guides, refer to the [documentation](https://docs.arkloop.ai).

## Key Features

**1. Agent Loop**
Autonomous multi-step execution with planning, reasoning, and tool orchestration. The agent maintains persistent memory across conversations -- system-level constraints, long-term facts, and session context.

**2. Intelligent Search**
Deep web search that synthesizes sources into structured answers with citations. Not a wrapper around search APIs -- it reads, reasons, and responds.

**3. Sandboxed Code Execution**
Isolated execution environments powered by Firecracker microVMs or Docker containers. Supports Python, data analysis, chart generation, and file operations with strict resource limits.

**4. Tool Providers**
Unified provider management for search and fetch tools. Configure platform defaults and account-level overrides without changing agent prompts.

**5. Custom Personas**
Define specialized agent configurations with distinct system prompts, tool sets, and behavioral tiers. Switch between general-purpose, research-focused, and domain-specific modes.

**6. Multi-Model Support**
Integrates with OpenAI, Anthropic, and any OpenAI-compatible provider. Smart retry with rate limit handling and provider-level response caching.

**7. Enterprise Console**
Admin dashboard for user management, persona configuration, LLM credential management, usage analytics, audit logs, and feature flags.

**8. ClawHub Registry**  
Search and import skills from ClawHub, with compatibility for OpenClaw skill folders and `SKILL.md` layouts. Arkloop syncs upstream security scan status during import and surfaces risk warnings in the Web UI.

## Architecture

| Service | Stack | Role |
|---------|-------|------|
| API | Go | Authentication, resource management, RBAC, audit logging |
| Gateway | Go | Reverse proxy, rate limiting, risk scoring, geo-IP |
| Worker | Go | Job execution, LLM routing, tool dispatch, persona management |
| Sandbox | Go | Code execution in Firecracker VMs or Docker containers |
| Bridge | Go | Project bridge service |
| Web | React / TypeScript | User-facing chat interface |
| Console | React / TypeScript | Platform administration dashboard |
| Console Lite | React / TypeScript | Lightweight administration dashboard (default) |

Infrastructure: PostgreSQL + PgBouncer, Redis, MinIO (S3-compatible), OpenViking (vector memory).

## Star Us

If you find Arkloop useful, give it a star -- it helps others discover the project.

<!-- Star GIF will be added here -->


## Developer Checks

For daily local validation, use the repository CI helper:

```bash
# Fast daily checks
bin/ci-local quick

# Go integration checks with a temporary PostgreSQL container
bin/ci-local integration

# Full local run
bin/ci-local full

# GitHub Actions style verification
bin/ci-local act go-check
bin/ci-local act typescript
```

Recommended order: `bin/ci-local quick` -> `bin/ci-local integration` -> `bin/ci-local act <job>`.
`quick` installs frontend dependencies automatically, so the first run can take longer.
`bin/ci-local act ...` pulls a large runner image on first use.
`bin/ci-local act go-integration` is currently not recommended; use `bin/ci-local integration` instead.

## Contributing

We welcome contributions. See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines on how to get involved.

## Contributors

<a href="https://github.com/qqqqqf/Arkloop/graphs/contributors">
  <img src="https://contrib.rocks/image?repo=qqqqqf/Arkloop" />
</a>

## Security

To report vulnerabilities, please email security@arkloop.ai instead of opening a public issue. See [SECURITY.md](SECURITY.md) for our disclosure policy.

## License

Licensed under the [Arkloop License](LICENSE), a modified Apache License 2.0 with additional conditions:

- **Multi-tenant restriction**: Source code may not be used to operate a multi-tenant SaaS without written authorization.
- **Brand protection**: LOGO and copyright information in the frontend components must not be removed or modified.

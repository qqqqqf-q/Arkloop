<p align="center">
  <img src="https://cdn.nodeimage.com/i/rvRvQavXMOx1ostIUyAqBc3mfy9SOGM4.png" alt="Arkloop" />
</p>

<h3 align="center">AI agents, without the clutter.</h3>

<p align="center">
  <a href="./docs/zh-CN/README.md"><img alt="简体中文" src="https://img.shields.io/badge/简体中文-d9d9d9"></a>
  <a href="./LICENSE"><img alt="License" src="https://img.shields.io/badge/license-Arkloop%20License-blue"></a>
  <a href="https://github.com/qqqqqf/Arkloop/graphs/commit-activity"><img alt="Commits" src="https://img.shields.io/github/commit-activity/m/qqqqqf/Arkloop?labelColor=%2332b583&color=%2312b76a"></a>
  <a href="https://github.com/qqqqqf/Arkloop/issues"><img alt="Issues closed" src="https://img.shields.io/github/issues-search?query=repo%3Aqqqqqf%2FArkloop%20is%3Aclosed&label=issues%20closed&labelColor=%237d89b0&color=%235d6b98"></a>
  <a href="https://twitter.com/intent/follow?screen_name=qqqqqf_"><img alt="Follow on X" src="https://img.shields.io/twitter/follow/qqqqqf_?logo=X&color=%20%23f5f5f5"></a>
</p>

---

Arkloop is an open-source AI agent platform that prioritizes design over dashboards. Multi-model routing, sandboxed execution, persistent memory -- all behind a clean interface that stays out of your way.

Available as a **desktop app** (macOS / Linux / Windows) and a self-hosted server.

## Download

Download the latest release from [GitHub Releases](https://github.com/qqqqqf/Arkloop/releases).

The desktop app bundles everything locally -- no Docker, no configuration. Just open and use.

## Self-Hosting

> Requires Docker, Docker Compose, and Python 3. 2+ CPU cores, 4+ GiB RAM.

```bash
git clone https://github.com/qqqqqf/Arkloop.git
cd Arkloop
./setup.sh install
```

For production deployment with pre-built images:

```bash
./setup.sh install --prod --non-interactive ...
```

See the full [installation guide](docs/installation.md) for configuration options.

## Features

**Desktop App** -- Native Electron app with a Go sidecar. Runs entirely on your machine with automatic updates via GitHub Releases.

**Multi-Model Routing** -- OpenAI, Anthropic, and any OpenAI-compatible provider. Priority-based routing with rate limit handling and provider-level caching.

**Sandboxed Execution** -- Firecracker microVMs (Linux) or Docker containers (macOS/Windows). Python, data analysis, chart generation with strict resource limits.

**Persistent Memory** -- System-level constraints, long-term facts, and session context that survive across conversations. Powered by OpenViking vector memory.

**Prompt Injection Protection** -- Semantic-level scanning that detects and blocks injection attempts. A feature most alternatives don't bother implementing.

**Channel Integration** -- Connect your agent to Telegram with full media support, group context handling, and rate limiting.

**ACP Integration** -- Agent Communication Protocol support for inter-agent coordination inside sandboxed environments.

**MCP Support** -- Model Context Protocol configuration for extending agent capabilities with external tools.

**Custom Personas** -- Define specialized agent configurations with distinct system prompts, tool sets, and behavioral tiers. Optional Lua scripting for custom agent loops.

**Skill Ecosystem** -- Search and import skills from ClawHub, compatible with OpenClaw `SKILL.md` layouts. Security scan status synced during import.

**Admin Console** -- User management, persona configuration, LLM credential management, usage analytics, audit logs, and feature flags.

## Architecture

| Service | Stack | Role |
|---------|-------|------|
| API | Go | Authentication, RBAC, resource management, audit logging |
| Gateway | Go | Reverse proxy, rate limiting, risk scoring, geo-IP |
| Worker | Go | Job execution, LLM routing, tool dispatch, agent loop |
| Sandbox | Go | Code execution in Firecracker VMs or Docker containers |
| Desktop | Electron + Go | Native desktop app with embedded sidecar |
| Web | React / TypeScript | User-facing chat interface |
| Console | React / TypeScript | Administration dashboard |

Infrastructure: PostgreSQL + PgBouncer, Redis, SeaweedFS (S3-compatible) 或 filesystem (默认), OpenViking (vector memory).

## Development

```bash
# Quick local CI check
bin/ci-local quick

# Go integration tests
bin/ci-local integration

# Full check
bin/ci-local full
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for commit conventions and development workflow.

## Contributing

We welcome contributions of all kinds.

Even if you're not a developer -- if something feels off, a bit of spacing, a color that doesn't sit right, any tiny detail or even a big-picture direction -- please [open an issue](https://github.com/qqqqqf/Arkloop/issues). We take every UX detail seriously, and your feedback makes Arkloop better for everyone.

## Contributors

<a href="https://github.com/qqqqqf/Arkloop/graphs/contributors">
  <img src="https://contrib.rocks/image?repo=qqqqqf/Arkloop" />
</a>

## Security

To report vulnerabilities, please email qingf622@outlook.com instead of opening a public issue. See [SECURITY.md](SECURITY.md) for our disclosure policy.

## License

Licensed under the [Arkloop License](LICENSE), a modified Apache License 2.0 with additional conditions:

- **Multi-tenant restriction**: Source code may not be used to operate a multi-tenant SaaS without written authorization.
- **Brand protection**: LOGO and copyright information in the frontend components must not be removed or modified.

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

> System requirements: Docker and Docker Compose installed, 2+ CPU cores, 4+ GiB RAM.

```bash
git clone https://github.com/qqqqqf/Arkloop.git
cd Arkloop
cp .env.example .env
# Edit .env -- set passwords, API keys, and LLM credentials
docker compose up -d
```

Once all services are healthy, access the web interface at `http://localhost:8000`.

For detailed configuration, environment variables, and production deployment guides, refer to the [documentation](https://docs.arkloop.ai).

## Key Features

**1. Agent Loop**
Autonomous multi-step execution with planning, reasoning, and tool orchestration. The agent maintains persistent memory across conversations -- system-level constraints, long-term facts, and session context.

**2. Intelligent Search**
Deep web search that synthesizes sources into structured answers with citations. Not a wrapper around search APIs -- it reads, reasons, and responds.

**3. Sandboxed Code Execution**
Isolated execution environments powered by Firecracker microVMs or Docker containers. Supports Python, data analysis, chart generation, and file operations with strict resource limits.

**4. Browser Automation**
Headless browser control integrated as a native agent tool. Web interaction, data extraction, and screenshot capture via Playwright.

**5. Custom Personas**
Define specialized agent configurations with distinct system prompts, tool sets, and behavioral tiers. Switch between general-purpose, research-focused, and domain-specific modes.

**6. Multi-Model Support**
Integrates with OpenAI, Anthropic, and any OpenAI-compatible provider. Smart retry with rate limit handling and provider-level response caching.

**7. Enterprise Console**
Admin dashboard for user management, persona configuration, LLM credential management, usage analytics, audit logs, and feature flags.

## Architecture

| Service | Stack | Role |
|---------|-------|------|
| API | Go | Authentication, resource management, RBAC, audit logging |
| Gateway | Go | Reverse proxy, rate limiting, risk scoring, geo-IP |
| Worker | Go | Job execution, LLM routing, tool dispatch, persona management |
| Sandbox | Go | Code execution in Firecracker VMs or Docker containers |
| Browser | Node.js | Playwright-based headless browser automation |
| Web | React / TypeScript | User-facing chat interface |
| Console | React / TypeScript | Platform administration dashboard |

Infrastructure: PostgreSQL + PgBouncer, Redis, MinIO (S3-compatible), OpenViking (vector memory).

## Star Us

If you find Arkloop useful, give it a star -- it helps others discover the project.

<!-- Star GIF will be added here -->

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

<p align="center">
  <img src="https://cdn.nodeimage.com/i/rvRvQavXMOx1ostIUyAqBc3mfy9SOGM4.png" alt="Arkloop" />
</p>

<h3 align="center">Open-source / Clean / Powerful — Your AI Agent Platform</h3>

<p align="center">
  <a href="./docs/zh-CN/README.md"><img alt="简体中文" src="https://img.shields.io/badge/简体中文-d9d9d9"></a>
  <a href="./LICENSE"><img alt="License" src="https://img.shields.io/badge/license-Arkloop%20License-blue"></a>
  <a href="https://github.com/qqqqqf-q/Arkloop/graphs/commit-activity"><img alt="Commits" src="https://img.shields.io/github/commit-activity/m/qqqqqf-q/Arkloop?labelColor=%2332b583&color=%2312b76a"></a>
  <a href="https://github.com/qqqqqf-q/Arkloop/issues"><img alt="Issues closed" src="https://img.shields.io/github/issues-search?query=repo%3Aqqqqqf-q%2FArkloop%20is%3Aclosed&label=issues%20closed&labelColor=%237d89b0&color=%235d6b98"></a>
  <a href="https://x.com/intent/follow?screen_name=qqqqqf_"><img alt="Follow on X" src="https://img.shields.io/twitter/follow/qqqqqf_?logo=X&color=%20%23f5f5f5"></a>
</p>

---

> **Early Access** — Arkloop is currently in early public access; all releases are Alpha. You may encounter bugs, data loss, API changes, or incomplete features. We are iterating rapidly, but stability has not been fully validated. If you are willing to use it at this stage and provide feedback, we greatly appreciate it.

Arkloop is a design-focused open-source AI agent platform. Multi-model routing, sandboxed execution, persistent memory — a clean desktop app, ready out of the box.

## Download

Download the latest version from [GitHub Releases](https://github.com/qqqqqf-q/Arkloop/releases), supporting macOS, Linux, and Windows.

The desktop app bundles the full runtime — no Docker, no configuration. Just open and use. Automatic updates via GitHub Releases.

### CLI via Homebrew

Homebrew installs the Arkloop CLI only:

```bash
brew install qqqqqf-q/arkloop/arkloop
```

<details>
<summary>macOS: app blocked (Move to Trash)</summary>

Do **not** click **Move to Trash** when macOS warns that **Arkloop** cannot be verified.

1. Choose **Done** or close the dialog.

   ![Gatekeeper blocked Arkloop](./docs/readme/macos-gatekeeper-01-not-opened.png)

2. Open **System Settings → Privacy & Security**. Under **Security**, find **Open Anyway** next to the **Arkloop** message and click it.

   ![Privacy & Security, Open Anyway](./docs/readme/macos-gatekeeper-02-privacy-security.png)

3. When the confirmation dialog appears, choose **Open Anyway** again.

   ![Confirmation dialog](./docs/readme/macos-gatekeeper-03-confirm-open.png)

</details>

## Contributing

We welcome contributions of all kinds.

Even if you're not a developer, just a regular user — if anything feels off while using it, even a bit of spacing, a color, a tiny detail, or a big-picture direction — please [open an issue](https://github.com/qqqqqf-q/Arkloop/issues). We take every UX detail seriously, and your feedback makes the experience better for everyone.

See [CONTRIBUTING.md](CONTRIBUTING.md) for commit conventions and development workflow.

## If you can, give us a Star
![wkwUSiE3xZw1NeDrSFqJYDkkSEDULMfu](https://cdn.nodeimage.com/i/wkwUSiE3xZw1NeDrSFqJYDkkSEDULMfu.gif)

## Features

Arkloop does what other AI chat tools do — multi-model support, tool calling, code execution, memory — but we focus on doing it cleanly:

- **Multi-Model Routing** — OpenAI, Anthropic, and any compatible API; priority-based automatic routing with rate limit handling
- **Sandboxed Execution** — Code runs in Firecracker microVMs or Docker containers with strict resource limits
- **Persistent Memory** — System constraints, long-term facts, and session context preserved across conversations
- **Prompt Injection Protection** — Semantic-level scanning that detects and blocks injection attacks
- **Channel Integration** — Telegram integration with media handling and group context
- **Custom Personas** — Independent system prompts, tool sets, and behavior configs; Lua scripting supported
- **MCP / ACP** — Model Context Protocol and Agent Communication Protocol support
- **Skill Ecosystem** — Import skills from ClawHub, compatible with OpenClaw SKILL.md format

Full documentation at [docs](https://arkloop.cn/en/docs/guide).

## Architecture

| Service | Stack | Role |
|---------|-------|------|
| API | Go | Authentication, RBAC, resource management, audit logging |
| Gateway | Go | Reverse proxy, rate limiting, risk scoring |
| Worker | Go | Job execution, LLM routing, tool dispatch, agent loop |
| Sandbox | Go | Code execution isolation |
| Desktop | Electron + Go | Native desktop app with embedded sidecar |
| Web | React / TypeScript | User interface |
| Console | React / TypeScript | Admin dashboard |

Infrastructure: PostgreSQL, Redis, SeaweedFS (or filesystem), OpenViking (vector memory).

## Development

```bash
bin/ci-local quick        # Quick local CI
bin/ci-local integration  # Go integration tests
bin/ci-local full          # Full check
```

## Self-Hosting

> The self-hosting deployment path is still in development. While included in the current release, availability is not guaranteed. We are not focusing on this during the Alpha phase. We plan to provide full server deployment support once the desktop version stabilizes.

## Contributors

<a href="https://github.com/qqqqqf-q/Arkloop/graphs/contributors">
  <img src="https://contrib.rocks/image?repo=qqqqqf-q/Arkloop" />
</a>

## Security

To report vulnerabilities, please email qingf622@outlook.com instead of opening a public issue. See [SECURITY.md](SECURITY.md) for our disclosure policy.

> Huh? You're asking why we use a .cn domain? I don't really want to either -- I'd long assumed I'd use an .io domain, but the budget fell short, so that's on hold for now.

## License

Licensed under the [Arkloop License](LICENSE), a modified Apache License 2.0 with additional conditions:

- **Multi-tenant restriction** — Source code may not be used to operate a multi-tenant SaaS without written authorization.
- **Brand protection** — LOGO and copyright information in the frontend components must not be removed or modified.

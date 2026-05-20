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
  <a href="https://t.me/Arkloop_io"><img alt="Telegram" src="https://img.shields.io/badge/Telegram-Group-blue?logo=telegram"></a>
  <a href="https://github.com/qqqqqf-q/Arkloop/stargazers"><img alt="Stars" src="https://img.shields.io/github/stars/qqqqqf-q/Arkloop?style=social"></a>
  <a href="https://github.com/qqqqqf-q/Arkloop/network/members"><img alt="Forks" src="https://img.shields.io/github/forks/qqqqqf-q/Arkloop?style=social"></a>
</p>

---

Arkloop is a design-focused open-source AI Agent platform. Multi-model routing, sandboxed execution, persistent memory — a clean desktop app that works out of the box.

## Download

Download the latest version from [GitHub Releases](https://github.com/qqqqqf-q/Arkloop/releases), supporting macOS, Linux, and Windows.

The desktop app bundles the full runtime — no Docker, no configuration. Just open and use. Automatic updates via GitHub Releases.

On first launch, Desktop can install the `ark` command-line tool. After that, you can start the same local runtime without the Desktop window:

```bash
ark web
```

### CLI via Homebrew

Homebrew installs the Arkloop CLI only:

```bash
brew install qqqqqf-q/arkloop/arkloop && ark web
```

For a headless Linux machine, use one command:

```bash
sh -c 'set -e; arch="$(uname -m)"; case "$arch" in x86_64|amd64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; *) echo "unsupported architecture: $arch" >&2; exit 1 ;; esac; name="ark-linux-${arch}"; rm -rf "$name"; curl -fsSL "https://github.com/qqqqqf-q/Arkloop/releases/latest/download/${name}.tar.gz" | tar -xz; cd "$name"; exec ./ark web --host 0.0.0.0 --no-open'
```

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

Full documentation at [docs](https://arkloop.io/en/docs/guide).


## Contributing

We welcome contributions of all kinds.

Even if you're not a developer, just a regular user — if anything feels off while using it, even a bit of spacing, a color, a tiny detail, or a big-picture direction — please [open an issue](https://github.com/qqqqqf-q/Arkloop/issues). We take every UX detail seriously, and your feedback makes the experience better for everyone.

See [CONTRIBUTING.md](CONTRIBUTING.md) for commit conventions and development workflow.

## Sponsors

Thanks to the following friends for their support, keeping Arkloop going:

- [@Jinnkunn](https://github.com/Jinnkunn) — Bought me a domain
- @jeck — Treated me to an iced Americano
- @chuichui — Covered my AI costs for two weeks
- [@薄荷奶昔](https://github.com/SkyAerope) — Covered AI costs for Clover and Chiffon


## Contributors

<a href="https://github.com/qqqqqf-q/Arkloop/graphs/contributors">
  <img src="https://contrib.rocks/image?repo=qqqqqf-q/Arkloop" />
</a>


## If you can, give us a Star
![wkwUSiE3xZw1NeDrSFqJYDkkSEDULMfu](https://cdn.nodeimage.com/i/wkwUSiE3xZw1NeDrSFqJYDkkSEDULMfu.gif)

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



## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=qqqqqf-q/Arkloop&type=date&legend=top-left)](https://www.star-history.com/#qqqqqf-q/Arkloop&type=date&legend=top-left)

## Security

To report vulnerabilities, please email qingf622@outlook.com instead of opening a public issue. See [SECURITY.md](SECURITY.md) for our disclosure policy.

## License

Licensed under the [Arkloop License](LICENSE), a modified Apache License 2.0 with additional conditions:

- **Multi-tenant restriction** — Source code may not be used to operate a multi-tenant SaaS without written authorization.
- **Brand protection** — LOGO and copyright information in the frontend components must not be removed or modified.

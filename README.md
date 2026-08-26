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

Arkloop is a personal open-source project: a local-first platform for conversational AI agents. Multi-model routing, persistent memory — a clean desktop app that works out of the box.

Everything runs locally. The whole backend is a single embedded process with a SQLite database — no servers to deploy, no infrastructure to maintain.

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

### CLI via AUR (Arch Linux)

```bash
yay -S arkloop-bin    # prebuilt binary
yay -S arkloop-git    # build from source
```

For a headless Linux machine, use one command:

```bash
sh -c 'set -e; arch="$(uname -m)"; case "$arch" in x86_64|amd64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; *) echo "unsupported architecture: $arch" >&2; exit 1 ;; esac; name="ark-linux-${arch}"; rm -rf "$name"; curl -fsSL "https://github.com/qqqqqf-q/Arkloop/releases/latest/download/${name}.tar.gz" | tar -xz; cd "$name"; exec ./ark web --host 0.0.0.0 --no-open'
```

## Features

Arkloop does what other AI chat tools do — multi-model support, tool calling, code execution, memory — but we focus on doing it cleanly:

- **Multi-Model Routing** — OpenAI, Anthropic, Gemini, and any OpenAI-compatible API; priority-based routing with your own keys
- **Agent Runtime** — Built-in tools, MCP servers, and ClawHub skills; sub-agent spawning and scheduled jobs
- **Memory** — Plain-text notebook by default; optional Nowledge semantic memory; can be turned off entirely
- **Channels** — Telegram, Discord, QQ, Feishu, and WeChat bots sharing the same agent pipeline, with scheduled heartbeat runs
- **Custom Personas** — Independent system prompts, tool allowlists, budgets, and executor types

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

One embedded Go process is the whole backend: API and worker are libraries, not separate services. Storage is a local SQLite database (auto-migrated on first start) plus the filesystem — no Postgres, Redis, or message queue.

| Piece | Stack | Role |
|-------|-------|------|
| Desktop | Electron | Native shell embedding the Go runtime |
| Runtime | Go | Single process: API + worker; SQLite, in-process event bus |
| Web | React / TypeScript | Chat UI, bundled into the desktop app and served by `ark web` |
| CLI | Go | `ark` — headless entrypoint to the same runtime |

## Development

```bash
pnpm install
cd src/apps/desktop && pnpm dev        # Desktop app (Electron + embedded runtime)

# Headless, from source:
cd src/apps/web && pnpm build
go run ./src/services/cli/cmd/ark web  # Serves the web UI and local API

bin/ci-local quick                     # Local CI
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for the full workflow.

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=qqqqqf-q/Arkloop&type=date&legend=top-left)](https://www.star-history.com/#qqqqqf-q/Arkloop&type=date&legend=top-left)

## Security

To report vulnerabilities, please email qingf622@outlook.com instead of opening a public issue. See [SECURITY.md](SECURITY.md) for our disclosure policy.

## License

Licensed under the [Arkloop License](LICENSE), a modified Apache License 2.0 with additional conditions:

- **Multi-tenant restriction** — Source code may not be used to operate a multi-tenant SaaS without written authorization.
- **Brand protection** — LOGO and copyright information in the frontend components must not be removed or modified.

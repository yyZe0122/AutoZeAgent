# YunmengZe Agent

[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![Status](https://img.shields.io/badge/status-alpha-orange.svg)](#status--状态)

A local-first, policy-controlled automation agent in Go.

本地优先、受策略约束的 Go 自动化智能体：持久任务、双轨会话（agent 可写 / plan 只读）、受控工具调用与可审计本地状态。

> **Alpha:** review config, policy, and permission boundaries before using important data or privileged credentials.  
> **Alpha 阶段：** 在接触重要数据或高权限凭据前，请先检查配置与权限边界。

## What you get / 生产形态

```text
ymzd   long-running daemon
ymz    local CLI + TUI (gatewayclient peers; TUI primary)
core.db        single SQLite source of truth
```

- **agent (build):** multi-turn chat, workspace read+write tools  
- **plan:** same chat loop, **read-only** tools (no separate Planner approval UX)  
- Effects only via **Tool Broker** (Policy → Approval → Grant → containment → limits → Audit)

Design KB: [`docs/architecture/`](docs/architecture/) · living backlog: [`docs/optimization/current.md`](docs/optimization/current.md)

![Architecture](docs/assets/architecture.svg)

## Install / 安装

**Recommended:** install via a package manager (binaries on PATH).  
**推荐：** 用包管理器安装（装到 PATH）。

Release notes: [`docs/changelog/`](docs/changelog/) · publication: [`docs/release.md`](docs/release.md).

### Package managers / 包管理器

**macOS / Linux** ([Homebrew](https://brew.sh)):

```bash
brew install --cask yyZe0122/tap/ymz
ymz
ymz version && ymzd --check
```

**Windows** ([Scoop](https://scoop.sh)):

```powershell
scoop bucket add ymz https://github.com/yyZe0122/scoop-bucket
scoop install ymz
ymz
ymz version
```

Tap / bucket are updated automatically on each GitHub Release (`homebrew-tap`, `scoop-bucket`).

### Fallback: one-line installer / 兜底：一键脚本

Use when brew/scoop is unavailable. Pins Pre-release tags (or omit `YMZ_VERSION` when a non-prerelease `latest` exists).

无 brew/scoop 时使用。Pre-release 请固定版本。

**Windows** → `%LOCALAPPDATA%\Programs\YunmengZe\bin` + user PATH:

```powershell
$env:YMZ_VERSION = 'v0.1.0'
irm "https://raw.githubusercontent.com/yyZe0122/YunmengZe-Agent/main/packaging/scripts/install.ps1" | iex
```

**Linux / macOS** → `~/.local/bin`:

```bash
export YMZ_VERSION=v0.1.0
curl -fsSL "https://raw.githubusercontent.com/yyZe0122/YunmengZe-Agent/main/packaging/scripts/install-user.sh" | sh
export PATH="$HOME/.local/bin:$PATH"
```

Optional: `YMZ_INSTALL_DIR`, `YMZ_REPOSITORY`.

**Manual zip/tar:** extract `ymz` / `ymzd` onto PATH. Naming: `ymz_{version}_{os}_{arch}.…` (`amd64` = x64). Prefer package managers above.

### From source / 源码

Go **1.26+**, pure Go SQLite (`CGO_ENABLED=0`).

```bash
make all          # check + build + daemon --check → bin/
make install      # ~/.local/bin
```

```powershell
.\scripts\dev.ps1 -Action all
.\scripts\dev.ps1 -Action install
```

Systemd / machine-wide install and **release publication**: [`docs/release.md`](docs/release.md), [`packaging/install/systemd.md`](packaging/install/systemd.md).

## Configure / 配置

Provider config lives in the **OS config directory only** (not project cwd). One-line installers seed templates when missing.

| Mode | Linux | Windows |
| --- | --- | --- |
| user | `~/.config/yunmengze/` | `%APPDATA%\YunmengZe\` |
| system | `/etc/yunmengze/` | `%ProgramData%\YunmengZe\config\` |

Lookup: `agent.local.json` then `agent.json`. Optional `env` file (`KEY=value`) is loaded by daemon/CLI (does not override already-set process env).

**API key — pick any (not forced):**

1. `{env:DEEPSEEK_API_KEY}` + system env or ConfigDir `env` file (**recommended**)  
2. `{file:relative-or-abs-path}`  
3. Literal `"apiKey": "sk-..."` in JSON (local only; protect permissions)

```bash
# after install, or manually:
# edit ~/.config/yunmengze/env   OR   export DEEPSEEK_API_KEY=...
# OR put a literal apiKey in agent.json
ymz config validate --mode user
```

Full multi-provider example: [`configs/agent.json.example`](configs/agent.json.example).

Minimal shape:

```json
{
  "model": "deepseek/deepseek-chat",
  "provider": {
    "deepseek": {
      "type": "openai-compatible",
      "options": {
        "baseURL": "https://api.deepseek.com",
        "apiKey": "{env:DEEPSEEK_API_KEY}"
      },
      "models": {
        "deepseek-chat": {
          "name": "DeepSeek Chat",
          "maxTokens": 4096,
          "contextWindow": 65536
        }
      }
    }
  },
  "models": {
    "subagent": "deepseek/deepseek-chat",
    "compact": "deepseek/deepseek-chat"
  },
  "chat": {
    "workspace": { "default": "client_cwd", "allow": [], "allow_all": false },
    "allow_write": true,
    "compaction": { "enabled": true },
    "max_iterations": 16,
    "permission": { "mode": "preauth" }
  }
}
```

- `maxTokens` = output cap; `contextWindow` = packing/UI pressure ([ADR-041](docs/architecture/041-context-packing-and-pressure.md), [`docs/provider-protocols.md`](docs/provider-protocols.md))
- Optional `models.subagent` / `models.compact` ([ADR-045](docs/architecture/045-model-roles.md))
- Optional `chat`: workspace, `tools.git` / `tools.process`, `permission.mode` (`preauth` \| `ask`), memory ([ADR-038](docs/architecture/038-session-chat-boundary.md), [043](docs/architecture/043-tool-call-permission-interaction.md), [044](docs/architecture/044-in-process-memory-boundary.md), [046](docs/architecture/046-session-workspace-and-permission-tiers.md))
- Full options: [`configs/agent.json.example`](configs/agent.json.example), [`configs/agent.schema.json`](configs/agent.schema.json)

## Run / 运行

**TUI is the primary UX.** No-arg / `ymz` opens it and ensures a unique local daemon (stop only via `daemon stop`).

```bash
ymz
# or: ymz
ymz daemon status
ymz daemon stop
```

`health` / most subcommands need a **running** daemon; only TUI entry and `run` call ensure.

### TUI

| Input | Behavior |
| --- | --- |
| **Tab** | Toggle **agent** (build · R/W) ↔ **plan** (read-only) |
| Plain text | Submit on current mode / focused session |
| `/help` | Full slash list |
| `/new [msg]` | Fresh session |
| `/sessions` · `/tasks` | List overlay |
| `/skills` | Toggle skills for next submit |
| `/model` | Model picker; `/model provider/model` switches |
| `/compact [focus]` | Force head compaction |
| `/perm` | Tool permissions (`ask`); `once\|similar\|permanent\|deny`; keys **1–4** |
| `/memory` | List/search facts; `forget\|promote <id>` |
| `/refresh-memory` | Rebuild frozen memory inject for next turn |
| `/cron` | Jobs; `/cron 15m <objective>` on focused session |
| `/status` | Health + model + task + context + pending perms |
| `/retry` · `/stop` | Resubmit last user msg · cancel |
| `/theme` | Day ↔ night |
| `/quit` | Exit (Ctrl+C clears input only) |

### CLI (secondary)

```bash
ymz health --mode user
ymz run --mode user "Report workspace status without changing files."
ymz task status TASK_ID --mode user
ymz logs --tail 200 --mode user
ymz logs --run RUN_ID
# YMZ_LOG_LEVEL=debug  →  ADR-047
ymz job list --mode user
ymz help
```

Prefer TUI `/cron` to create jobs; CLI `job create` is for scripts.

## Architecture (short) / 架构要点

```text
User → CLI/TUI → local Gateway → ymzd
         → chatsession (agent|plan) · agent · tool broker · skills · jobs
         → providers / controlled effects · core.db
```

| Piece | Role |
| --- | --- |
| CLI · TUI | Gateway clients only — no tools, providers, or grants |
| Gateway | HTTP/UDS adapter; does not execute tools or call models |
| Tool Broker | Only model-requested effect path |
| Jobs | Fixed-interval chat submits ([ADR-042](docs/architecture/042-chat-native-jobs.md)) |

Start here: [`docs/architecture/README.md`](docs/architecture/README.md).

## Development / 开发

```bash
make format && make check && make build
go test ./... -count=1
```

```powershell
.\scripts\dev.ps1 -Action format
.\scripts\dev.ps1 -Action check
```

See [`CONTRIBUTING.md`](CONTRIBUTING.md), [`AGENTS.md`](AGENTS.md). Report vulns via [`SECURITY.md`](SECURITY.md).

## Security notes / 安全

- Do not commit secrets, `agent.local.json`, `*.db`, logs, sockets, or `bin/` / `dist/`.
- Least privilege: workspace roots, `chat.tools`, service accounts.
- Grants and the Tool Broker are security boundaries, not optional UI.
- Back up `core.db` before upgrades on important installs.
- Review remote install scripts before piping to a shell.

Details: [`SECURITY.md`](SECURITY.md), [threat model ADR-008](docs/architecture/008-threat-model.md).

## License / 协议

[Apache License 2.0](LICENSE). Contributions under the same terms.

## Status / 状态

Alpha. Production shape is the three-piece stack above; ordered UX backlog T1–T7 and structured logging L1 are landed — see [`docs/optimization/current.md`](docs/optimization/current.md) for optional tails only.

Release / private-to-public checklist: [`docs/release.md`](docs/release.md).

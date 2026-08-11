# AutoZeAgent

[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![Status](https://img.shields.io/badge/status-alpha-orange.svg)](#status--状态)

A local-first, policy-controlled automation agent in Go.

本地优先、受策略约束的 Go 自动化智能体：持久任务、双轨会话（agent 可写 / plan 只读）、受控工具调用与可审计本地状态。

> **Alpha:** review config, policy, and permission boundaries before using important data or privileged credentials.  
> **Alpha 阶段：** 在接触重要数据或高权限凭据前，请先检查配置与权限边界。

## What you get / 生产形态

```text
autozeagentd   long-running daemon
autozeagent    local CLI + TUI (gatewayclient peers; TUI primary)
core.db        single SQLite source of truth
```

- **agent (build):** multi-turn chat, workspace read+write tools  
- **plan:** same chat loop, **read-only** tools (no separate Planner approval UX)  
- Effects only via **Tool Broker** (Policy → Approval → Grant → containment → limits → Audit)

Design KB: [`docs/architecture/`](docs/architecture/) · living backlog: [`docs/optimization/current.md`](docs/optimization/current.md)

![Architecture](docs/assets/architecture.svg)

## Install / 安装

**Prebuilt (recommended):** [latest release](https://github.com/yyZe0122/AutoZeAgent/releases/latest) — `autozeagent`, `aze`, `autozeagentd` + `checksums.txt`.

| Platform | AMD64 | ARM64 |
| --- | --- | --- |
| Windows | `autozeagent_windows_amd64.zip` | `autozeagent_windows_arm64.zip` |
| Linux | `autozeagent_linux_amd64.tar.gz` | `autozeagent_linux_arm64.tar.gz` |
| macOS | `autozeagent_darwin_amd64.tar.gz` | `autozeagent_darwin_arm64.tar.gz` |

### One-line (public releases) / 一键安装（公开 Release）

```powershell
# Windows (user PATH; no admin)
irm "https://raw.githubusercontent.com/yyZe0122/AutoZeAgent/main/packaging/scripts/install.ps1" | iex
```

```bash
# Linux / macOS → ~/.local/bin
curl -fsSL "https://raw.githubusercontent.com/yyZe0122/AutoZeAgent/main/packaging/scripts/install-user.sh" | sh
export PATH="$HOME/.local/bin:$PATH"
```

Optional: `AUTOZEAGENT_VERSION`, `AUTOZEAGENT_INSTALL_DIR`.

```bash
autozeagent version && autozeagentd --check
```

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

Provider config lives in the **OS config directory only** (not project cwd):

| Mode | Linux | Windows |
| --- | --- | --- |
| user | `~/.config/autozeagent/` | `%APPDATA%\AutoZeAgent\` |
| system | `/etc/autozeagent/` | `%ProgramData%\AutoZeAgent\config\` |

Lookup: `autozeagent.local.json` then `autozeagent.json`. Prefer `{env:VAR}` / `{file:…}` over literal keys.

```bash
mkdir -p ~/.config/autozeagent
cp configs/autozeagent.json.example ~/.config/autozeagent/autozeagent.json
chmod 600 ~/.config/autozeagent/autozeagent*.json
export DEEPSEEK_API_KEY='…'   # match your provider block
autozeagent config validate --mode user
```

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
- Full options: [`configs/autozeagent.json.example`](configs/autozeagent.json.example), [`configs/autozeagent.schema.json`](configs/autozeagent.schema.json)

## Run / 运行

**TUI is the primary UX.** No-arg / `aze` opens it and ensures a unique local daemon (stop only via `daemon stop`).

```bash
aze
# or: autozeagent
autozeagent daemon status
autozeagent daemon stop
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
autozeagent health --mode user
autozeagent run --mode user "Report workspace status without changing files."
autozeagent task status TASK_ID --mode user
autozeagent logs --tail 200 --mode user
autozeagent logs --run RUN_ID
# AUTOZEAGENT_LOG_LEVEL=debug  →  ADR-047
autozeagent job list --mode user
autozeagent help
```

Prefer TUI `/cron` to create jobs; CLI `job create` is for scripts.

## Architecture (short) / 架构要点

```text
User → CLI/TUI → local Gateway → autozeagentd
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

- Do not commit secrets, `autozeagent.local.json`, `*.db`, logs, sockets, or `bin/` / `dist/`.
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

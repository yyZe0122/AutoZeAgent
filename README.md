# YunmengZe Agent

[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![Status](https://img.shields.io/badge/status-alpha-orange.svg)](#status)

Local-first automation agent in Go: durable tasks, dual-track chat (**agent** R/W · **plan** read-only), Tool Broker effects, single SQLite.

本地优先的 Go 自动化智能体：持久任务、双轨会话、受控工具、单库 `core.db`。

> **Alpha** — review config, workspace roots, and permissions before privileged use.  
> **Alpha** — 接触重要数据或高权限凭据前，请先核对配置与权限边界。

## Quick start

```bash
# 1) Install (pick one)
brew install --cask yyZe0122/tap/ymz          # macOS / Linux
# scoop bucket add ymz https://github.com/yyZe0122/scoop-bucket && scoop install ymz   # Windows

# 2) API key (recommended)
#    edit ~/.yunmengze/env  →  DEEPSEEK1_API_KEY=sk-...
#    or: export DEEPSEEK1_API_KEY=...

# 3) Validate + open TUI (auto-starts daemon)
ymz config validate --mode user
ymz
```

Leave the TUI with `/quit` — **daemon keeps running**. Stop it with `ymz stop`.

```text
ymzd     long-running daemon
ymz      CLI + TUI (TUI is primary)
core.db  single SQLite source of truth
```

![Architecture](docs/assets/architecture.svg)

## Features

| | |
| --- | --- |
| **Dual-track chat** | **agent** = build with workspace tools · **plan** = same loop, read-only |
| **Tool Broker** | Only effect path: Policy → Approval → Grant → path limits → Audit |
| **Local daemon** | Unique per mode; TUI/`run` auto-ensure; `ymz start\|stop\|restart\|status` |
| **Multi-provider** | Nested catalog per supplier; select `providerId/modelId…` (OpenCode-style) |
| **Hot-reload** | Main provider stack (~0.5s) while daemon runs — [ADR-048](docs/wiki/adr/048-provider-config-hot-reload.md) |
| **OpenCode import** | `ymz config import-opencode` → `agent.local.json` (MCP local+remote, `chat.commands`, compaction; warn+drop plugins/LSP) |
| **Memory · skills · cron · MCP** | In-process memory (`default_ttl` + expired soft-archive), skill drafts + Hermes `skills_list`/`skill_view` + unused archive ([ADR-050](docs/wiki/adr/050-in-process-self-improvement.md)), `AGENTS.md` rules, chat-native jobs, stdio/remote MCP via Broker |
| **Slash templates** | `chat.commands` → `/<cmd> [args]` expands `$ARGUMENTS` (instruction only; no grants) |
| **Session model** | `/model prefer` stores preference; chat runs resolve **job pin → prefer → main** (global `/model` unchanged) |

Docs: [`docs/README.md`](docs/README.md) · wiki: [`docs/wiki/`](docs/wiki/) · backlog: [`docs/backlog/current.md`](docs/backlog/current.md) · releases: [`docs/history/changelog/`](docs/history/changelog/)

## Install

### Package managers (recommended)

**macOS / Linux** ([Homebrew](https://brew.sh)):

```bash
brew install --cask yyZe0122/tap/ymz
ymz version && ymzd --check
```

**Windows** ([Scoop](https://scoop.sh)):

```powershell
scoop bucket add ymz https://github.com/yyZe0122/scoop-bucket
scoop install ymz
ymz version
```

Taps update on each GitHub Release.

### Fallback installers

Pin Pre-release tags (or omit `YMZ_VERSION` when a non-prerelease `latest` exists).

**Windows** → `%LOCALAPPDATA%\Programs\YunmengZe\bin` + user PATH:

```powershell
$env:YMZ_VERSION = 'v0.2.6'
irm "https://raw.githubusercontent.com/yyZe0122/YunmengZe-Agent/main/packaging/scripts/install.ps1" | iex
```

**Linux / macOS** → `~/.local/bin`:

```bash
export YMZ_VERSION=v0.2.6
curl -fsSL "https://raw.githubusercontent.com/yyZe0122/YunmengZe-Agent/main/packaging/scripts/install-user.sh" | sh
export PATH="$HOME/.local/bin:$PATH"
```

Optional: `YMZ_INSTALL_DIR`, `YMZ_REPOSITORY`. Manual zip/tar: put `ymz` / `ymzd` on PATH (`ymz_{version}_{os}_{arch}`).

### From source

Go **1.26+**, pure Go SQLite (`CGO_ENABLED=0`).

```bash
make all && make install    # check + build + ~/.local/bin
```

```powershell
.\scripts\dev.ps1 -Action all
.\scripts\dev.ps1 -Action install
```

Systemd / publish: [`docs/release.md`](docs/release.md), [`packaging/install/systemd.md`](packaging/install/systemd.md).

## Configure

Config lives under a **flat home root** (not project cwd). Installers seed templates when missing.

| Mode | Path |
| --- | --- |
| **user** (all OS) | **`~/.yunmengze/`** — Windows: `%USERPROFILE%\.yunmengze\` |
| system | Linux `/etc/yunmengze` · Win `%ProgramData%\YunmengZe\config` |

```text
~/.yunmengze/
  agent.json          # or agent.local.json (wins)
  env                 # optional KEY=value (does not override process env)
  core.db
  logs/  run/  skills/
```

Override root: `YMZ_HOME=/abs/path`.

### API key (any one)

1. `{env:DEEPSEEK1_API_KEY}` + system env or `~/.yunmengze/env` (**recommended**)  
2. `{file:path}` relative to ConfigDir  
3. Literal `"apiKey": "sk-..."` in JSON (local only; mode `600`)

```bash
ymz paths user
ymz config validate --mode user
# Optional: map OpenCode config → agent.local.json (warnings for plugins/LSP/oauth MCP)
ymz config import-opencode              # default ~/.config/opencode/opencode.json
ymz config import-opencode ./opencode.json --dry-run
```

### Minimal provider config

Templates use two sample suppliers: **`deepseek1`** (official bare model ids) and **`deepseek2`** (gateway / nested wire ids). Active selection below is `deepseek1`.

```json
{
  "model": "deepseek1/deepseek-chat",
  "provider": {
    "deepseek1": {
      "type": "openai-compatible",
      "options": {
        "baseURL": "https://api.deepseek.com/v1",
        "apiKey": "{env:DEEPSEEK1_API_KEY}"
      },
      "models": {
        "deepseek-chat": {
          "name": "DeepSeek Chat",
          "maxTokens": 4096,
          "contextWindow": 65536
        }
      }
    },
    "deepseek2": {
      "type": "openai-compatible",
      "options": {
        "baseURL": "https://llm.example.com/v1",
        "apiKey": "{env:DEEPSEEK2_API_KEY}"
      },
      "models": {
        "deepseek/deepseek-v4-flash": {
          "name": "Nested wire id → select deepseek2/deepseek/deepseek-v4-flash"
        },
        "flash": {
          "name": "Short key + id override → select deepseek2/flash",
          "id": "deepseek/deepseek-v4-flash"
        }
      }
    }
  }
}
```

- Selection is **`providerId/modelId…`** (first `/` only; model segment may contain `/`). Catalog keys match that segment; optional `models.<key>.id` overrides the wire/API id (OpenCode-style).  
- Example: `deepseek1/deepseek-chat` wires `deepseek-chat`; `deepseek2/deepseek/deepseek-v4-flash` wires `deepseek/deepseek-v4-flash`.  
- `maxTokens` = output cap; `contextWindow` = packing / UI pressure ([ADR-041](docs/wiki/adr/041-context-packing-and-pressure.md)).  
- Optional role map `models.subagent` / `models.compact` ([ADR-045](docs/wiki/adr/045-model-roles.md)).  
- Optional `chat` (workspace, tools, permission, memory, **commands**): full example [`configs/agent.json.example`](configs/agent.json.example) · wire formats [`docs/wiki/provider-protocols.md`](docs/wiki/provider-protocols.md).  
- User rules: `~/.yunmengze/AGENTS.md` (seeded if missing; do not overwrite existing). Project `.yunmengze/AGENTS.md` is appended when present. Instruction only — no grants.  
- Optional `mcp.servers`: stdio (`command`) or remote (`type`/`url`/`headers`) — [ADR-040](docs/wiki/adr/040-mcp-tool-broker.md).

### Hot-reload

While the daemon is up, edits to `agent.json` / `agent.local.json` / `env` rebuild the **main** provider client (~0.5s). In-flight turns keep the old client.

| Reloads live | Needs `ymz restart` |
| --- | --- |
| model, baseURL, protocol, literal / `{file:}` key | `chat.*`, MCP, `models.subagent\|compact` |
| `{env:VAR}` when process VAR is still empty | process env already set; or daemon started without agent |

Details: [ADR-048](docs/wiki/adr/048-provider-config-hot-reload.md).

## Run

```bash
ymz                 # TUI (auto-starts daemon)
ymz start           # daemon only
ymz status          # JSON
ymz restart         # stop + start
ymz stop            # shut down daemon
# long form: ymz daemon start|stop|restart|status
```

`health` / most CLI subcommands need a **running** daemon; only TUI and `run` call ensure.

### TUI

Chat transcript uses **rounded bubbles** (user / assistant / thinking / tool), not a log dump. Assistant replies with markdown markers render via **glamour** (streaming is throttled; unclosed fences stay plain). Foldable blocks: `/expand` or keys **`e`** (last) · **`E`** (all) · **`c`** (collapse); click a folded card when mouse is supported.

| Input | Behavior |
| --- | --- |
| **Tab** · **Shift+Tab** | **agent** (R/W) ↔ **plan** (read-only); Tab also completes slash |
| Plain text | Submit on current mode / session |
| `/help` | Slash list + keys |
| `/new` · `/sessions` · `/tasks` | New session; list sessions; list / focus tasks |
| `/back` · `/clear` | Session list (`/clear` aliases `/back`) |
| `/model` | Switch **global** main (`/model provider/model`); `/model prefer [ref]` session prefer (next chat run) |
| `/skills` · `/<skill-id>` | Multi-select preload, or skill-as-slash (instruction only). Model otherwise uses `skills_list` → `skill_view`. `/skills apply\|reject <id>` · `/skills archived` |
| `/<cmd> [args]` | `chat.commands` template slash (`$ARGUMENTS`); priority: built-in → commands → skill |
| `/compact` · `/perm` | Context compact; tool permission (H4 may hint prior once/similar) |
| `/memory` · `/refresh-memory` | Facts (`/memory archived` · `forget\|promote <id>`); rebuild frozen inject |
| `/expand` · `/journey` | Fold/expand; prepend memory and/or skill-event timeline (`/journey skills`) |
| `/cron` | Jobs on focused session |
| `/pause` · `/resume` · `/cancel` · `/stop` | Task control (`/stop` = `/cancel`) |
| `/status` · `/retry` · `/theme` | Health · resubmit last user message · day/night theme |
| `/quit` | Exit TUI (`/q` `/exit`; daemon stays up) |
| **e** / **E** / **c** | Expand last foldable · expand all · collapse (empty input) |

### CLI (scripts)

```bash
ymz version
ymz tui --mode user
ymz paths user
ymz health --mode user
ymz run --mode user --execution-mode plan "Report workspace status without changing files."
ymz task status TASK_ID --mode user
ymz task pause|resume|cancel TASK_ID --mode user
ymz logs --tail 200 --run RUN_ID --session SESSION_ID --task TASK_ID
ymz job list --mode user
ymz job create --session SESSION_ID --name NAME --title TITLE --every 1h "objective"
ymz job status|pause|resume|cancel JOB_ID --mode user
ymz db check --mode user
```

Prefer TUI `/cron` to create jobs. Logs: `YMZ_LOG_LEVEL=debug` · [ADR-047](docs/wiki/adr/047-structured-logging-and-debug-chain.md).

## Architecture

```text
User → CLI / TUI → local Gateway → ymzd
         → chatsession · agent · tool broker · skills · jobs
         → providers · core.db
```

| Piece | Role |
| --- | --- |
| CLI · TUI | Gateway clients only — no tools, providers, or grants |
| Gateway | HTTP/UDS adapter; no tool execution, no model calls |
| Tool Broker | Sole model-requested effect path |
| Jobs | Fixed-interval chat submits ([ADR-042](docs/wiki/adr/042-chat-native-jobs.md)) |

Start: [`docs/README.md`](docs/README.md) · [`docs/wiki/README.md`](docs/wiki/README.md).

## Development

```bash
make format && make check && make build
go test ./... -count=1
```

```powershell
.\scripts\dev.ps1 -Action format
.\scripts\dev.ps1 -Action check
```

See [`CONTRIBUTING.md`](CONTRIBUTING.md), [`AGENTS.md`](AGENTS.md). Vulns: [`SECURITY.md`](SECURITY.md).

## Security

- Do not commit secrets, `agent.local.json`, `*.db`, logs, sockets, or `bin/` / `dist/`.
- Least privilege: workspace roots, `chat.tools`, service accounts.
- Grants and the Tool Broker are security boundaries, not optional UI.
- Back up `core.db` before upgrades on important installs.
- Review remote install scripts before piping to a shell.

Details: [`SECURITY.md`](SECURITY.md), [threat model ADR-008](docs/wiki/adr/008-threat-model.md).

## License

[Apache License 2.0](LICENSE). Contributions under the same terms.

## Status

Alpha. Production shape is the three-piece stack above. TUI marsh palette + three-column flatten shipped in **v0.2.6**. Remaining tails (O5–O6 / H2 / M*) in [`docs/backlog/current.md`](docs/backlog/current.md).

Release checklist: [`docs/release.md`](docs/release.md).

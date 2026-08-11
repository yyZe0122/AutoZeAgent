# Release and publication

Moved out of the root README so install/run docs stay short. Chinese notes follow each block where useful.

从根 README 外移：日常安装/运行见仓库根目录 README；本页仅发布、资产命名与公开前审核。

## Asset naming / 资产命名

GoReleaser builds **one archive per OS/arch**. Each archive contains **three binaries** (`autozeagent`, `aze`, `autozeagentd`) plus configs and packaging scripts.

每个平台一个归档；归档内含 **三个二进制** 与配置/脚本。

| Pattern | Example (tag `v0.1.0`) |
| --- | --- |
| `autozeagent_{version}_{os}_{arch}.tar.gz` | `autozeagent_0.1.0_linux_amd64.tar.gz` |
| `autozeagent_{version}_windows_{arch}.zip` | `autozeagent_0.1.0_windows_amd64.zip` |
| `checksums.txt` | SHA-256 of all archives (fixed name) |

- `{version}` = tag **without** leading `v` (GoReleaser `.Version`).
- Installers (`packaging/scripts/install-user.sh`, `install.ps1`) must stay in sync with this pattern.
- Prefer `AUTOZEAGENT_VERSION=v0.1.0` when the release is marked **Pre-release** (GitHub `latest` may skip it).

## Release notes / 更新日志

1. Add `docs/changelog/vX.Y.Z.md` (bilingual body: highlights, asset table, verify, install).
2. Tag must match the file name: tag `v0.1.0` → `docs/changelog/v0.1.0.md`.
3. Publish uses: `goreleaser release … --release-notes=docs/changelog/${tag}.md`.
4. Missing notes file **fails** the publish script / CI on purpose.

## One-shot publish (root) / 一键发布

**Default = local build + upload** (no GitHub Actions minutes). Only **root** may commit/tag/push on this host (scheme A).

```bash
sudo -i
cd /home/yyze/projects/AutoZeAgent

# PAT with repo (or fine-grained: contents read/write on this repo). Never commit the token.
export GITHUB_TOKEN=ghp_...

./scripts/publish-release.sh v0.1.0
# uncommitted release pipeline files:
./scripts/publish-release.sh v0.1.0 --commit-paths release
# tag already on HEAD (e.g. after a failed Actions attempt):
./scripts/publish-release.sh v0.1.0 --upload-only
```

Script: [`scripts/publish-release.sh`](../scripts/publish-release.sh)

| Flag | Meaning |
| --- | --- |
| *(default)* | `make check` → optional snapshot → push main/tag → **local** `goreleaser release` upload |
| `--upload-only` | Skip push; HEAD must already be tag; local upload only |
| `--via-actions` | Push tag only; let GitHub Actions publish (**needs billing OK**) |
| `--commit-paths release` | Commit goreleaser/changelog/installers/README whitelist |
| `--commit-paths all --yes` | Commit entire dirty tree (careful) |
| `--snapshot-only` | `make check` + snapshot only |
| `--force-tag --yes` | Replace existing local/remote tag |
| `--dry-run` | Print steps |

Requires `docs/changelog/vX.Y.Z.md` and `GITHUB_TOKEN` for the default path.

### Auth for local upload / 本地上传鉴权

**推荐（root）：`gh auth login`**，脚本会自动 `gh auth token` → `GITHUB_TOKEN`。

```bash
# install gh (root), then:
gh auth login
# GitHub.com → HTTPS → Login with a web browser (or paste token)
# When asked for scopes / fine-grained: need Contents + Workflows write
# Classic login scopes must include: repo, workflow, read:org (as offered)
```

Or export a PAT yourself:

| Type | Required access |
| --- | --- |
| **Fine-grained** | This repo; **Contents: R/W**; **Workflows: R/W** (needed if tag commit touches `.github/workflows/`); Metadata: Read |
| **Classic** | **`repo` + `workflow`** |

Why **Workflows**? `POST /repos/.../releases` returns `403 Resource not accessible by personal access token` when the target commit modifies workflow files and the token cannot. Our pipeline often changes `release.yml`.

```bash
# optional manual token
export GITHUB_TOKEN='…'
# probe read (200) and create draft release (201, not 403) — see script 403 help
```

If the Release page only shows **Source code** zip/tar.gz, binaries were never uploaded (Actions billing lock and/or failed local token). Use local upload with a correct PAT, not `--via-actions`, until billing is healthy.

### Optional: GitHub Actions / 可选 CI

[`.github/workflows/release.yml`](../.github/workflows/release.yml) still runs on `v*` tags when Actions is enabled and billing is healthy. Prefer **local upload** when hosted runners are unavailable.

### Local snapshot only / 仅本地快照

```bash
goreleaser release --snapshot --clean --parallelism 1
# dist/autozeagent_<snapshot-version>_{os}_{arch}.*
```

### Authenticated download / 私密下载

```bash
gh auth login
gh release download v0.1.0 --repo yyZe0122/AutoZeAgent
```

## Private-to-public audit checklist / 转公开清单

Complete every item before changing repository visibility.

更改可见性前完成全部项：

- Every reachable branch/tag starts from the intended sanitized root commit; no legacy history.
  - 可达分支/标签均始于预期净化根提交，无旧历史。
- API keys, local config, databases, logs, caches, personal docs, machine paths, editor/agent workspaces are untracked and ignored (see root `.gitignore`).
  - 密钥、本地配置、库、日志、缓存、个人文档、机器路径、编辑器工作区均未跟踪且已忽略。
- Full tests, `go mod verify`, installer syntax checks, GoReleaser validation, six-platform snapshot build.
  - 完整测试、依赖校验、安装脚本语法、GoReleaser、六平台快照构建。
- Scan reachable Git history, Actions logs/artifacts, release notes, archives, and `checksums.txt` for secrets/PII.
  - 扫描历史与产物中的密钥与个人数据。
- Authorized tester downloads every private release asset, verifies checksums, inspects layout, runs smoke checks.
  - 授权测试员下载并校验每个私密 Release 产物。
- Replace placeholders, review GitHub settings/permissions, and read [repository visibility docs](https://docs.github.com/repositories/managing-your-repositorys-settings-and-features/managing-repository-settings/setting-repository-visibility) before switching to public.
  - 替换占位符、复核仓库设置，转公开前阅读 GitHub 可见性文档。

## Linux system install (brief) / 系统安装（摘要）

Machine-wide systemd: extract a Linux release archive, then from the extract dir:

```bash
sudo sh packaging/scripts/install.sh .
sudo install -m 0640 -o root -g autozeagent \
  configs/autozeagent.json.example /etc/autozeagent/autozeagent.json
# Optional env file for {env:…} secrets (legacy name; not a Planner process):
#   /etc/autozeagent/planner.env
sudo systemctl enable --now autozeagent
```

Details: [`packaging/install/systemd.md`](../packaging/install/systemd.md).

Also see root [README](../README.md) and [v0.1.0 notes](changelog/v0.1.0.md).

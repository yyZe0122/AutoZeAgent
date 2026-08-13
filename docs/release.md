# Release and publication

日常安装/运行见根 [README](../README.md)。**本页是唯一发版操作手册**——按下面默认路径执行即可，不必再向 agent 单独确认流程。

This page is the **only** release runbook. Follow the default path; do not invent parallel steps.

---

## Default path (this host) / 默认一键发版

**Scheme A:** only **root** may commit / tag / push / upload on this machine.

### Batch commits before push (required)

Do **not** squash a multi-feature dirty tree into one `release:` commit via `--commit-paths all`.

1. Split the working tree into **feature-sized commits** (one concern each: e.g. injectscan, live MD, permission hints, skill list/view, skill draft/archive, AGENTS inject, docs).
2. Each commit must compile and keep tests green (`make check` at least once before the first push).
3. **Push those commits**, then publish.

`--commit-paths all` is only for a leftover that is already one logical change (typically `docs/history/changelog/vX.Y.Z.md` after the feature commits are on `main`).

### Every release (copy-paste)

```bash
sudo -i
cd /home/yyze/projects/AutoZeAgent

# Auth once per machine (preferred): gh auth login
# Or export for this shell:
#   export GITHUB_TOKEN=...            # main repo Contents (+ Workflows if touching .github)
#   export PACKAGE_GITHUB_TOKEN=...    # homebrew-tap + scoop-bucket Contents R/W

# 1) Feature commits already on main (see above). Changelog MUST exist:
#    docs/history/changelog/vX.Y.Z.md

# 2) Clean main (or only changelog leftover) → tag + local GoReleaser upload
./scripts/publish-release.sh vX.Y.Z --yes

# Changelog-only leftover (one commit), then tag + upload:
# ./scripts/publish-release.sh vX.Y.Z \
#   --commit-paths all --yes \
#   --message "docs(changelog): vX.Y.Z"
```

Replace `vX.Y.Z` with the real tag (e.g. `v0.2.2`). Script runs `make check`, creates annotated tag, pushes `main` + tag, then **local** `goreleaser release` (not GitHub Actions minutes).

### Pre-flight checklist

| Step | Action |
| --- | --- |
| 1 | **Batch-commit** the working tree by feature (not one mega `release:` dump). Push those commits. |
| 2 | Bump / write **`docs/history/changelog/vX.Y.Z.md`** (bilingual highlights, asset table, install). Missing file **fails** publish. |
| 3 | No secrets in tree: no `agent.local.json`, `*.db`, `env` with real keys, `bin/`, tokens in docs. |
| 4 | `make check` green (script runs it unless `--skip-check`). |
| 5 | `gh auth login` or valid `GITHUB_TOKEN` + `PACKAGE_GITHUB_TOKEN`. |
| 6 | Run `./scripts/publish-release.sh vX.Y.Z` as **root** (clean tree). Use `--commit-paths all` only for changelog leftover. |

### After publish

```bash
gh release view vX.Y.Z --repo yyZe0122/YunmengZe-Agent
# Must list platform archives + checksums.txt (not only Source code zip)

gh api repos/yyZe0122/homebrew-tap/commits --jq '.[0].commit.message'
gh api repos/yyZe0122/scoop-bucket/commits --jq '.[0].commit.message'
```

Users:

```bash
brew upgrade --cask ymz
# or
export YMZ_VERSION=vX.Y.Z
curl -fsSL "https://raw.githubusercontent.com/yyZe0122/YunmengZe-Agent/main/packaging/scripts/install-user.sh" | sh
```

### Common variants

| Situation | Command |
| --- | --- |
| Only snapshot (no tag/upload) | `./scripts/publish-release.sh vX.Y.Z --snapshot-only` |
| Tag already on HEAD; re-upload assets | `./scripts/publish-release.sh vX.Y.Z --upload-only` |
| Move tag to current HEAD | `./scripts/publish-release.sh vX.Y.Z --force-tag --yes` |
| Print steps only | `./scripts/publish-release.sh vX.Y.Z --dry-run` |
| Push tag, let Actions publish | `./scripts/publish-release.sh vX.Y.Z --via-actions` (needs billing OK) |

Script: [`scripts/publish-release.sh`](../scripts/publish-release.sh).

---

## User install channels / 用户安装渠道

| Channel | Platforms | Command |
| --- | --- | --- |
| **Homebrew** (recommended) | macOS, Linux | `brew install --cask yyZe0122/tap/ymz` |
| **Scoop** (recommended) | Windows | `scoop bucket add ymz https://github.com/yyZe0122/scoop-bucket` then `scoop install ymz` |
| One-line scripts (fallback) | Win / Linux / macOS | `install.ps1` / `install-user.sh` |
| Manual / source | all | Release zip/tar or `make install` |

Affiliate repos (auto-updated by GoReleaser on each tag):

- [`yyZe0122/homebrew-tap`](https://github.com/yyZe0122/homebrew-tap) → `Casks/ymz.rb`
- [`yyZe0122/scoop-bucket`](https://github.com/yyZe0122/scoop-bucket)

## Asset naming / 资产命名

GoReleaser builds **one archive per OS/arch**. Each archive contains **two binaries** (`ymz`, `ymzd`) plus configs and packaging scripts.

| Pattern | Example (tag `v0.2.2`) |
| --- | --- |
| `ymz_{version}_{os}_{arch}.tar.gz` | `ymz_0.2.2_linux_amd64.tar.gz` |
| `ymz_{version}_windows_{arch}.zip` | `ymz_0.2.2_windows_amd64.zip` |
| `checksums.txt` | SHA-256 of all archives (fixed name) |

- `{version}` = tag **without** leading `v` (GoReleaser `.Version`).
- Prefer `YMZ_VERSION=vX.Y.Z` when the release is **Pre-release** (GitHub `latest` may skip it).

## Release notes / 更新日志

1. Add `docs/history/changelog/vX.Y.Z.md` (bilingual: highlights, asset table, verify, install).
2. Tag must match the file name: tag `v0.2.2` → `docs/history/changelog/v0.2.2.md`.
3. Publish uses: `goreleaser release … --release-notes=docs/history/changelog/${tag}.md`.
4. Missing notes file **fails** the publish script / CI on purpose.

## Auth / 鉴权

**推荐（root）：`gh auth login`**，脚本会自动 `gh auth token` → `GITHUB_TOKEN`。

| Token | Required access |
| --- | --- |
| **`GITHUB_TOKEN`** | **YunmengZe-Agent**: Contents R/W; Workflows R/W if commit touches `.github/workflows` |
| **`PACKAGE_GITHUB_TOKEN`** | **homebrew-tap** + **scoop-bucket**: Contents R/W |
| **Classic** (one token) | **`repo` + `workflow`** |

If the Release page only shows **Source code** zip/tar.gz, binaries never uploaded — fix PAT and use default local upload (not `--via-actions` while billing is broken).

### Optional: GitHub Actions

[`.github/workflows/release.yml`](../.github/workflows/release.yml) runs on `v*` tags when Actions + billing are healthy. Prefer **local upload** when runners are unavailable. Repo secret `PACKAGE_GITHUB_TOKEN` required for brew/scoop from CI.

### Local snapshot only

```bash
export PACKAGE_GITHUB_TOKEN=dummy
goreleaser release --snapshot --clean --parallelism 1 --skip=publish
```

## Private-to-public audit checklist / 转公开清单

- Reachable branch/tag history is intentional; no legacy secrets in history.
- Secrets, local config, `*.db`, logs, caches untracked (root `.gitignore`).
- Full tests, `go mod verify`, installer checks, GoReleaser six-platform snapshot.
- Scan history, Actions logs, release notes, archives, `checksums.txt`.
- Authorized tester downloads every asset and smoke-tests.
- Review visibility settings before going public.

## Linux system install (brief)

```bash
sudo sh packaging/scripts/install.sh .
sudo install -m 0640 -o root -g ymz \
  configs/agent.json.example /etc/yunmengze/agent.json
sudo systemctl enable --now ymz
```

Details: [`packaging/install/systemd.md`](../packaging/install/systemd.md).

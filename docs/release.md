# Release and publication

Moved out of the root README so install/run docs stay short. Chinese notes follow each block where useful.

从根 README 外移：日常安装/运行见仓库根目录 README；本页仅发布与公开前审核。

## Private-first workflow / 私密优先

The first GitHub repository must stay **private** until CI, package builds, secret scans, history review, and manual inspection of release assets all pass.

首个仓库须保持**私密**，直到 CI、安装包构建、密钥扫描、历史审查和 Release 产物人工检查全部通过。

While private, only authorized collaborators should test cloned source and private release assets. Root README one-line installers that use `raw.githubusercontent.com` are for **public** releases, not the private review path.

私密期间仅授权协作者测试；README 中基于 `raw.githubusercontent.com` 的一键安装面向**公开** Release。

### Local release matrix / 本地发布矩阵

Build the full matrix before creating a GitHub Release:

```bash
goreleaser release --snapshot --clean --parallelism 1
```

### Authenticated private download / 私密产物下载

```bash
gh auth login
gh release download v0.1.0 --repo yyZe0122/AutoZeAgent
```

### Tag and publish / 打标签发布

Only after the target commit passes the audit checklist below:

```bash
git tag -a v0.1.0 -m "AutoZeAgent v0.1.0"
git push origin v0.1.0
```

（push 权限按本机约定：可读 yyze、推送 root。）

The tag workflow re-runs verification, builds six platform archives, writes `checksums.txt`, and publishes a GitHub Release (private while the repo is private).

标签工作流会再次验证、生成六平台包与 `checksums.txt`，并发布 Release（仓库私密时产物亦受限）。

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
- Replace placeholders, review GitHub settings/permissions, read [repository visibility docs](https://docs.github.com/repositories/managing-your-repositorys-settings-and-features/managing-repository-settings/setting-repository-visibility) before switching to public.
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

Also see root [README](../README.md) for user install and run.

# AutoZeAgent systemd 安装与运维

正式单元文件为 `packaging/systemd/autozeagent.service`。systemd 只管理一个长期运行的 `autozeagentd`；`autozeagent` 是按需执行的本地 CLI。Task、Run 和 Job 都是 `core.db` 中的逻辑对象，不需要额外 systemd unit。

## 构建发布目录

```bash
make build-linux-amd64
cp -R packaging dist/linux-amd64/
```

`dist/linux-amd64` 发布目录需要两个二进制：`autozeagent` 和 `autozeagentd`，以及仓库中的 `packaging` 文件。生产架构不再安装 Memory、Scheduler、Evolution 或 Echo 模块进程。

## 安装

```bash
sudo sh packaging/scripts/install.sh dist/linux-amd64
sudo systemctl enable --now autozeagent
```

脚本创建无登录权限的 `autozeagent` 用户、受限配置/状态/日志目录，安装两个二进制和 systemd unit，并执行 `systemctl daemon-reload`。

服务启动前执行：

```bash
autozeagent config validate --mode system
```

启动后 unit 通过本地 Gateway 执行：

```bash
autozeagent health --mode system
```

Linux/macOS Gateway 使用 `/run/autozeagent/autozeagent.sock`，运行目录由 systemd 创建并限制权限。`autozeagentd --check` 只验证 bootstrap、`core.db` 和 Scheduler Store，不创建在线 Gateway 文件。

Unit 使用 `Restart=on-failure`、专用用户、`RuntimeDirectory`、`StateDirectory`、`LogsDirectory`、`NoNewPrivileges`、`ProtectSystem=strict`、明确的 `ReadWritePaths`、`LimitNOFILE` 和 30 秒停止超时。

## Provider 与密钥

Provider 配置放在系统配置目录的 `autozeagent.json`，密钥使用 `{env:NAME}` 或 `{file:path}` 引用。Unit 当前可选读取 `/etc/autozeagent/planner.env` 作为环境变量来源；该文件名为兼容保留，不代表独立 Planner 进程。

## 验证与日志

```bash
sudo systemctl status autozeagent
sudo journalctl -u autozeagent -f
sudo -u autozeagent /usr/local/bin/autozeagent config validate --mode system
sudo -u autozeagent /usr/local/bin/autozeagent health --mode system
sudo -u autozeagent /usr/local/bin/autozeagent db check --mode system
```

`health` 是通过本地 Socket 的在线检查；`db check` 是离线 SQLite 检查。生产事实源只有 `/var/lib/autozeagent/core.db`，Artifact 文件保存在同一状态目录下。

## 备份、恢复与升级

```bash
sudo sh packaging/scripts/backup.sh /var/backups/autozeagent/manual.tar.gz
sudo sh packaging/scripts/restore.sh /var/backups/autozeagent/manual.tar.gz
sudo sh packaging/scripts/upgrade.sh /path/to/new-release
```

备份会在复制 SQLite 文件前停止服务，并在结束时恢复原运行状态。恢复拒绝绝对路径、`..` 或 AutoZeAgent 目录之外的归档成员。升级先生成备份，再安装、校验和重启。

## 卸载

```bash
sudo sh packaging/scripts/uninstall.sh
```

默认保留 `/etc/autozeagent`、`/var/lib/autozeagent` 和日志。只有显式设置 `PURGE_CONFIG=1` 或 `PURGE_DATA=1` 才清理。卸载脚本仍会删除旧版本遗留的模块二进制，但不会重新安装或运行它们。

## 仍需 Linux 实机复验

Windows 开发机已经完成 Go 测试、健康检查和 Linux amd64 无 CGo 交叉构建。systemd 自动重启、Unix Socket 的真实 UID/GID 与权限、长时间运行、断电恢复和真实 Provider 网络故障仍须在 Linux 目标服务器执行本页命令验证。

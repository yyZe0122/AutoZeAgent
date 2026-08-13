# YunmengZe Agent systemd 安装与运维

正式单元文件为 `packaging/systemd/yunmengze.service`（服务名 **`yunmengze`**，`ExecStart` 为 `ymzd`）。systemd 只管理一个长期运行的 `ymzd`；`ymz` 是按需执行的本地 CLI。Task、Run 和 Job 都是 `core.db` 中的逻辑对象，不需要额外 systemd unit。

## 构建发布目录

```bash
make build-linux-amd64
cp -R packaging dist/linux-amd64/
```

`dist/linux-amd64` 发布目录需要两个二进制：`ymz` 和 `ymzd`，以及仓库中的 `packaging` 文件。生产架构不再安装 Memory、Scheduler、Evolution 或 Echo 模块进程。

## 安装

```bash
sudo sh packaging/scripts/install.sh dist/linux-amd64
sudo systemctl enable --now yunmengze
```

脚本创建无登录权限的 `yunmengze` 用户、受限配置/状态/日志目录，安装两个二进制和 systemd unit，并执行 `systemctl daemon-reload`。

服务启动前执行：

```bash
ymz config validate --mode system
```

启动后 unit 通过本地 Gateway 执行：

```bash
ymz health --mode system
```

Linux/macOS Gateway 使用 `/run/yunmengze/ymz.sock`，运行目录由 systemd 创建并限制权限。`ymzd --check` 只验证 bootstrap、`core.db` 和 Scheduler Store，不创建在线 Gateway 文件。

Unit 使用 `Restart=on-failure`、专用用户、`RuntimeDirectory`、`StateDirectory`、`LogsDirectory`、`NoNewPrivileges`、`ProtectSystem=strict`、明确的 `ReadWritePaths`、`LimitNOFILE` 和 30 秒停止超时。

## Provider 与密钥

Provider 配置放在系统配置目录的 `agent.json`，密钥使用 `{env:NAME}` 或 `{file:path}` 引用。Unit 可选读取 `/etc/yunmengze/env`（`EnvironmentFile=-`：文件不存在不失败）。

## 验证与日志

```bash
sudo systemctl status yunmengze
sudo journalctl -u yunmengze -f
sudo -u yunmengze /usr/local/bin/ymz config validate --mode system
sudo -u yunmengze /usr/local/bin/ymz health --mode system
sudo -u yunmengze /usr/local/bin/ymz db check --mode system
```

`health` 是通过本地 Socket 的在线检查；`db check` 是离线 SQLite 检查。生产事实源只有 `/var/lib/yunmengze/core.db`，Artifact 文件保存在同一状态目录下。

## 备份、恢复与升级

```bash
sudo sh packaging/scripts/backup.sh /var/backups/yunmengze/manual.tar.gz
sudo sh packaging/scripts/restore.sh /var/backups/yunmengze/manual.tar.gz
sudo sh packaging/scripts/upgrade.sh /path/to/new-release
```

备份会在复制 SQLite 文件前停止服务，并在结束时恢复原运行状态。恢复拒绝绝对路径、`..` 或 YunmengZe 目录之外的归档成员。升级先生成备份，再安装、校验和重启。

## 卸载

```bash
sudo sh packaging/scripts/uninstall.sh
```

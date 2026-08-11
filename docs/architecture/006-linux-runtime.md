# ADR-006：Linux 运行模型

- 状态：Accepted
- 日期：2026-07-13
- 更新：2026-07-21

## 决策

Linux 生产环境只长期运行一个 `ymzd` 进程，并由 systemd 负责启动、停止、失败重启和资源限制。`ymz` 是按需运行的本地 CLI，不是后台服务。

Task、Plan、Run 和 Job 都是 `core.db` 中的持久化领域对象，不对应独立操作系统进程。Scheduler heartbeat 在 `ymzd` 内运行；只有实际执行进程类 Tool 时才创建受控子进程。

系统模式使用 `/etc/yunmengze`、`/var/lib/yunmengze`、`/run/yunmengze` 和 `/var/log/yunmengze`。用户模式遵循 XDG 路径。生产路径中不再存在 ModuleDir 或独立模块数据目录。

结构化日志写入标准错误和日志目录，systemd/journald 负责服务级采集。Unit 使用 `Restart=on-failure`；daemon 重启后从 `core.db` 恢复可继续的 Task/Run/Job 状态。

## 验证边界

Windows 可完成测试、健康检查和无 CGo 的 Linux 交叉构建；systemd 自动重启、Unix Socket 权限、真实 UID/GID、长期运行和断电恢复仍必须在 Linux 目标机复验。

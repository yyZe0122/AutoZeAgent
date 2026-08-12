# ADR-037：CLI 自动确保唯一本地 Daemon

- 状态：Accepted
- 日期：2026-07-30

## 决策

`ymz` / `ymz` 在进入 **TUI** 或执行 **`run`** 前，通过 `internal/daemonctl` 确保本 `--mode` 下唯一的 `ymzd` 已就绪：

1. 探测本地 Gateway（`gateway.json` + `/v1/health`）；
2. 未就绪则在旁路启动 `ymzd --mode <mode>`（与 CLI 同目录或 `PATH`）；
3. 等待 Gateway 健康后再继续；
4. **退出 TUI / 结束 `run` 不会停止 daemon**；
5. 仅 `ymz stop` / `ymz daemon stop`（或进程信号 / systemd）关闭服务。

单实例仍由 Gateway 绑定保证：`ensureNoActiveEndpoint` + Unix socket / loopback 端口互斥。Daemon 在 Gateway 监听成功后写入 `RuntimeDir/ymzd.pid`，停止时删除；`stop` 对 PID 发 `SIGTERM`（Windows 用 `taskkill`）。

显式控制（短命令优先；`daemon …` 为等价长形式）：

```text
ymz start|stop|restart|status [--mode user|system]
ymz daemon start|stop|restart|status [--mode user|system]
```

`restart` = `stop`（未运行则视为成功）再 `start`。

## Provider 配置热更新

见 [ADR-048](048-provider-config-hot-reload.md)。摘要：

- 监视 ConfigDir 下 `agent.json` / `agent.local.json` / `env`；debounce 后重载 **main** provider 栈。
- 不热更 `chat.*`、MCP、`models.subagent|compact`；启动时未建起 chat 时修配置仍需 `ymz restart`。

## 非目标

- CLI 不嵌入 daemon 业务逻辑，不直接打开 `core.db` 执行任务；
- 不恢复多进程 Module Supervisor；
- 不把 systemd 用户服务作为唯一启动路径（可选运维方式仍可用）。

## 后果

- 用户可一句话 `ymz` 进入 TUI，无需先手动起守护进程；
- 后台常驻直到 `ymz stop`；
- `health` / `task` 等子命令仍假定 Gateway 可用，不自动 ensure（避免隐式拉起影响诊断）。

## 与 TUI 的关系

TUI 在 `ensureDaemon` 成功后调用 `tui.Run`；UI 内所有读写经 `gatewayclient`（`tui.Gateway`）打 Gateway。退出 TUI（仅 `/quit` 等斜杠）不停止 daemon。包边界与 backlog 见 `docs/optimization/current.md`。

# ADR-017：进程内 Scheduler 与 Core Task 桥接

## 状态

Accepted，2026-07-14；2026-07-21 更新为进程内实现；**2026-08-03 产品语义由 ADR-042 定为 chat-native Job（已落地）。**

## 决策

Scheduler 是 `autozeagentd` 内的一个后台 Runner，不是独立模块或操作系统进程。它使用 Core 已打开的 `*sql.DB`，Job、Job Run 和 Lease 与其他领域数据共同保存在 `core.db`。

Scheduler 只负责“何时提交任务”，不能创建 Approval、Grant、Agent Run 或 Tool Call。到期 Job 通过 SQLite claim/lease 领取，再调用 `tasksubmission.Service` 创建幂等 Core Task。Chat 产品路径见 **ADR-042**（双轨 agent/plan chatsession）；副作用仍经 Tool Broker。

当前支持固定秒数间隔、暂停、恢复、归档、lease 恢复、retry/backoff，以及 `run_once`、`skip`、`catch_up` misfire 策略。ACK 只确认 Core 是否已经接管 Task，并保存稳定的 Core Task ID。

Scheduler heartbeat 与 daemon 使用同一个取消上下文。daemon 停止时 heartbeat 退出；异常中断遗留的 lease 到期后可重新领取。Cron 表达式解析暂缓，直到有明确用户故事需要。

## 接口边界

Gateway 与客户端暴露 Job 创建、列表、状态和 pause/resume/cancel。**TUI `/cron` 为主**；CLI 为次要脚本入口。它们调用 Scheduler Store 的窄方法，不获得通用模块调用或直接 SQL 能力。

# 进程内 Scheduler 验证

Scheduler 直接运行在 daemon 内并使用 `core.db`；不存在独立 `scheduler.db` 或 `autozeagent-scheduler` 二进制。

Windows 验证：

```powershell
go test -count=1 ./internal/scheduler ./internal/scheduledtasks ./internal/gateway ./cmd/autozeagent ./cmd/autozeagentd
./scripts/dev.ps1 -Action all
```

验收点：

- Core migration 创建 Job、Run 和 Lease 表；
- 到期 Job 可被原子 claim，重复 heartbeat 不重复提交；
- lease 到期后可恢复；
- ACK 成功保存稳定 Core Task ID 并清理 lease；
- ACK 失败按 retry/backoff 或 misfire 策略推进；
- Scheduler 通过 `tasksubmission.Service` 创建 Task；
- waiting-approval Task 不被当作失败重试；
- Gateway 与 CLI 的 Job action 行为一致；
- daemon 启动 heartbeat，停止时通过 context 正常退出。

Cron 表达式不在当前范围；固定间隔已经覆盖长期心跳与周期任务的最小需求。

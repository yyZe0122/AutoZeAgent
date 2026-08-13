# 进程内 Scheduler 验证

Scheduler 直接运行在 daemon 内并使用 `core.db`；不存在独立 `scheduler.db` 或 `ymz-scheduler` 二进制。产品语义见 ADR-042（chat-native Job）。

Windows 验证：

```powershell
go test -count=1 ./internal/scheduler ./internal/scheduledtasks ./internal/gateway ./cmd/ymz ./cmd/ymzd
./scripts/dev.ps1 -Action all
```

Linux/macOS：

```bash
go test -count=1 ./internal/scheduler ./internal/scheduledtasks ./internal/gateway ./cmd/ymz ./cmd/ymzd
make check
```

验收点：

- Core migration 创建 Job、Run、Lease 表，并含 `execution_mode` / `skill_ids`；
- 到期 Job 可被原子 claim，重复 heartbeat 不重复提交；
- lease 到期后可恢复；
- ACK `task_created` 保存稳定 Core Task ID 并清理 lease；
- ACK 失败按 retry/backoff 或 misfire 策略推进；
- `scheduledtasks` 经 `tasksubmission.Service` 提交 **chat** task（agent 默认 / plan 可选）；
- Gateway `POST /v1/jobs` 创建成功；list/pause/resume/cancel 可用；
- TUI `/cron` list + create（当前 session + draft mode）；CLI create 为次要入口；
- daemon 启动 job runner，停止时通过 context 正常退出。

Cron 表达式不在当前范围；固定间隔覆盖周期 chat 任务的最小需求。

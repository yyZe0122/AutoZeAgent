# AutoZeAgent 长期运行 Agent 优化方案

更新：2026-07-21

## 1. 目标形态

AutoZeAgent 采用最小可生产架构：

```text
CLI / 本地调用方
        |
        v
     autozeagentd
        |
 Session / Task / Plan / Run
        |
  进程内 Agent Runner
        |
    Tool Broker
        |
 Approval / Capability Grant
        |
 File / Git / Process / HTTP
        |
      core.db
```

生产发布只包含：

```text
autozeagentd   一个长期运行守护进程
autozeagent    一个本地 CLI
core.db    一个 SQLite 事实源
```

`Task` 表示持久目标，`Run` 表示可恢复执行尝试；二者都不是独立操作系统进程。未来子代理优先建模为带 `parent_run_id` 的逻辑子 Run，在同一 daemon 内使用 goroutine 执行，并继承预算、审批、授权、取消和审计约束。

## 2. 已实施优化

### P0：可重复验证基线

验证链路：

```text
gofmt check -> go vet -> go test -> build -> autozeagentd --check
```

开发脚本只构建 `autozeagent` 与 `autozeagentd`，健康检查使用隔离的工作区数据目录。

### P1：暂停、取消与重启恢复

已完成：

- Task 新增 `paused` 状态；
- CLI 和 Gateway 支持 pause/resume/cancel；
- pause/cancel 可取消正在进行的 Provider 与 Tool 调用；
- paused Run 保持可恢复记录，resume 后由 runner 继续；
- canceled Task 不再被 worker 领取，并撤销该 Task 的 Grant；
- Agent Provider/Tool 记录持久化到 `core.db`；
- fake Provider + 真实 RecordStore/Tool 持久化测试证明 daemon 重启后不会重复成功 Tool Call，并能继续后续 Provider 回合。

仍需环境验证：

- 真实外部 Provider 的断网、限流和恢复；
- Linux systemd kill/restart 与断电场景。

### P2：进程内 Scheduler

已完成：

- Scheduler 表迁入 `core.db`；
- Scheduler Store 直接使用 Core 的 `*sql.DB`；
- `autozeagentd` 启动单个 Scheduler heartbeat；
- SQLite claim/lease、lease 到期恢复和幂等 Core Task ID；
- Scheduler 通过 `tasksubmission.Service` 创建 Task；
- Job Gateway API 与 `autozeagent job create|list|status|pause|resume|cancel`；
- ACK 正确处理 waiting-approval Task；
- interval、retry/backoff 及 `run_once|skip|catch_up` misfire 策略；
- Scheduler → Task 垂直集成测试。

暂缓：

- cron 表达式；
- 分布式 Scheduler；
- 每个 Job 独立进程或 systemd unit。

固定间隔已经覆盖长期心跳和周期任务的当前需求。

### P3：执行预算与重试

已完成：

- Agent 级 Provider turn 与 Tool call 上限；
- Run 与 Plan 级 duration、token、cost 预算；
- Provider 响应超出预算时不执行其中的 Tool Call；
- usage 持久化，重启不能绕过预算；
- 只有 `ProviderError{Retryable:true}` 才重试；
- Provider 最多尝试三次，默认等待 100ms、200ms；
- `RetryAfter` 最高按 5 秒处理；
- context 取消可中止等待和调用；
- 精确达到 token 上限可完成当前响应，但不能开始下一回合；
- `MaxCostMicros == 0` 表示不限额或 Provider 未报告成本，正数成本上限严格执行。

## 3. 已删除的复杂度

以下内容已从生产实现、构建或启动链路删除：

- `autozeagent-module-echo`；
- `autozeagent-scheduler`、`autozeagent-memory`、`autozeagent-evolution` 独立进程；
- Scheduler 独立 Service 包装、`scheduler.db` 和专用 migration runner；
- Memory、Evolution、Evolution Activation 未进入当前主链路的实现；
- Module Runtime、Registry、RPC、Supervisor、Manifest 和协议版本协商；
- 无消费者的 Event Dispatcher、delivery offsets；
- `/v1/modules` 与 ModuleDir；
- 构建、安装和示例配置中的模块入口。

Core migration 013 清理 `module_registry`、`module_offsets` 与旧 Evolution Activation 表。历史 migration 保留原序号，保证已有 `core.db` 可顺序升级。

## 4. 必须保留的安全边界

简化不等于绕过控制。以下能力继续保留：

- Tool Broker 是模型请求执行的唯一入口；
- Approval 与 Capability Grant；
- Canonical Plan Hash 绑定；
- symlink/junction 路径越界防护；
- 进程超时、输出上限、环境变量白名单和取消；
- Provider Secret Resolver 与日志脱敏；
- SQLite 持久化、Event Store 与 Audit；
- Task/Run/Job 幂等 ID 与恢复语义；
- 文件型 Skill Catalog 和每 Task Skill 快照。

## 5. 后续顺序

只在真实需求出现时继续：

1. Linux 目标机长期运行与 systemd 故障注入；
2. 真实 Provider 端到端 Task 验证；
3. CLI 交互闭环优化；
4. 基于 `parent_run_id` 的逻辑子 Run；
5. 明确需求出现后再增加 cron；
6. MCP/LSP 只通过 Tool Broker 增加窄适配器。

当前不建议引入：通用模块框架、Actor 系统、消息队列、ORM、DI 容器、工作流 DSL、Judge Agent 或多数据库事务。

## 6. Go 简单精神

- 领域对象使用具体结构体；
- 并发使用 `context.Context`、goroutine 和明确 owner；
- 一个组合根直接装配依赖；
- 一个 SQLite 数据库承担持久事实源；
- 小接口只放在调用方；
- 错误显式返回，恢复语义由测试固定；
- 新抽象必须由当前用户故事证明。

## 7. 当前验收状态

已通过：

- [x] Task pause/resume/cancel；
- [x] Agent 持久化与重启恢复；
- [x] Provider/Tool 取消与有界重试；
- [x] Scheduler heartbeat 与 daemon 恢复；
- [x] token/cost/duration/turn/tool 预算；
- [x] 发布命令仅 `autozeagentd` 与 `autozeagent`；
- [x] 事实源仅 `core.db`；
- [x] 生产启动链路无通用 Module Runtime；
- [x] `go test -count=1 ./...`；
- [x] `go vet ./...`；
- [x] `scripts/dev.ps1 -Action check`；
- [x] `scripts/dev.ps1 -Action all`；
- [x] `bin/autozeagentd.exe --check`；
- [x] Windows 原生构建与 Linux amd64 `CGO_ENABLED=0` 交叉构建；
- [x] `git diff --check` 无空白错误，仅有现存换行符转换警告。

环境级待办：

- [ ] 真实 Provider 完整 Task；
- [ ] Linux systemd 长期运行、Socket 权限、失败重启和断电恢复。

# ADR-012：Tool Broker 与执行边界

- 状态：Accepted
- 日期：2026-07-13
- 更新：2026-07-30

## 背景

AutoZeAgent 允许 Planner、Agent Runner 和 Gateway 触发的执行路径提出工具调用，但这些组件不能因为获得了一个 Skill、提示词或模型返回的 Tool Call 就直接产生副作用。执行前必须由代码统一校验 Plan、Policy、Capability Grant、超时和审计要求；否则“先执行后确认”会重新出现。

## 决策

### 唯一组合入口

Core 中所有内置工具只通过 `internal/tools.RegisterBuiltins` 创建并注册到 Tool Broker。文件、进程、Git 和 HTTP 工具的构造函数保持包内不可见，外部包不能取得内置 Tool 实例后直接调用 `Execute`。

低层进程执行器位于 `internal/tools/internal/executor`。Go 的嵌套 `internal` 导入规则在编译期禁止 Planner、Gateway、Agent 和其它 Core 包导入该执行器。`RegisterBuiltins` 接收公开的 `ExecutorConfig`，但具体 Runner 只在 tools 包内部创建和持有。

因此，正常 Core 调用链固定为：

```text
Caller -> Tool Broker -> Policy -> Capability Grant -> Audit(started)
       -> typed Tool -> bounded executor/IO -> Audit(final) -> Artifact
```

### 执行前检查

Tool Broker 必须在调用 Tool 的 `Execute` 之前完成以下操作：

1. 校验 Tool Request 必填 ID、参数 JSON 和超时。
2. 从注册表读取不可变 Tool Definition，并验证风险等级和输入 Schema 元数据。
3. 调用 Tool 的 `Authorization`，把参数归一化为 Capability、路径、命令参数或网络域名。
4. 使用 Policy 对风险等级作 Fail Closed 决策。
5. 对需要审批的调用，原子校验并消耗 Capability Grant。
6. 成功写入 `started` Audit；如果审计写入失败，则不得执行 Tool。

Grant 在执行前消耗。工具启动失败、返回非零退出码或被取消时不返还次数，避免相同授权被并发或重试重复使用。

### 输出与失败

stdout、stderr 和 Tool JSON 输出均有大小上限。超过 Broker 阈值的 JSON 输出写入内容寻址 Artifact Store，Tool Call 只保留 Artifact 引用。Tool 返回 `output + error` 时仍保留受限输出，便于诊断失败；无效 JSON 输出被视为执行失败。

每次调用至少记录 `started` 和最终结果之一：`succeeded`、`failed`、`denied`、`timed_out` 或 `cancelled`。Tool Call 状态与 Grant ID 持久化在 `core.db`。

### 平台执行策略

- Windows 使用 Job Object，并启用 `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`；取消或超时时终止整个 Job。`taskkill /T /F` 和直接 Kill 仅作为回退。
- Linux/Unix 使用独立 process group；取消或超时时向负 PID 发送 `SIGKILL`，终止整组进程。
- 命令名和参数以 `exec.Command(command, args...)` 传递，不拼接 shell 字符串。
- 工作目录必须为允许根目录中的绝对路径。
- 子进程环境只继承 allowlist 中的变量。
- Linux 可配置 UID/GID；生产部署仍应优先使用专用服务账户和 systemd 权限边界。

### 文件与网络边界

文件工具和 Grant 校验都使用解析符号链接后的 containment 检查。已存在路径部分逐组件解析 symlink；Windows 上同时识别 junction/reparse link，再拼接尚不存在的后缀，从而拒绝通过 root 内链接访问 root 外目标。

HTTP 工具只允许 `http` 和 `https`，禁止 URL userinfo 和自动重定向。重定向目标必须作为新 URL 重新进入审批流程。网络 Grant 绑定归一化后的 hostname。

## 安全边界与限制

路径预检查与实际打开文件之间仍存在 TOCTOU 窗口。当前实现降低了常见 `..` 和 symlink 逃逸风险，但不能替代操作系统级 mount namespace、只读 bind mount 或基于文件句柄的安全打开策略。

Go 的导入边界阻止 Core 代码误用低层执行器，但不能约束已经获得任意本机代码执行权限的恶意进程。生产路径**不**加载第三方模块进程；不可信代码不得链接进 `autozeagentd`。历史上的进程外模块方案已删除（见 ADR-001）。

HTTP 当前限制审批域名，不等同于完整 SSRF 防护。后续还需要 DNS/IP 策略、私网地址策略和连接阶段的地址复核。

## 结果

- Planner、Gateway 与 Agent 在编译期无法导入进程执行器。
- 内置副作用工具只能由 Broker 拥有的注册流程创建。
- Plan、Policy、Grant、Audit、Timeout 和 Artifact 形成统一强制链路。
- Skill 目录与进程内 Scheduler 不参与执行授权；删除它们不会削弱 Broker 边界。
- Session chat 预授权（ADR-038）仍走本链路，仅改变 Grant 如何签发，不改变执行校验。

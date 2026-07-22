# ADR 028：Core 标识类型与跨层所有权

## 状态

Accepted，2026-07-16。

## 背景

Kernel 已分别定义 Task、Plan、Step 与 Run ID，Approval 也定义 Approval ID，但这些类型由不同领域包拥有，Core 查询 DTO 和部分应用接口仍退化为普通 `string`。这允许在编译期把 Task ID 误传给 Plan 查询，也使查询层若要复用领域 ID 就必须依赖包含持久化实现的领域包。

标识需要跨 Kernel、Approval、CoreQuery、应用服务与 Gateway 传递，但可选模块的公开协议仍必须保持独立，不能导入 Core 的 `internal/*`。

## 决策

新增无依赖的 `internal/coreidentity`，集中定义：

- `SessionID`；
- `TaskID`；
- `PlanID`；
- `StepID`；
- `RunID`；
- `ApprovalID`；
- `GrantID`。

Kernel 和 Approval 通过类型别名继续暴露 `kernel.TaskID`、`kernel.PlanID`、`approval.ApprovalID` 等领域名称，因此现有领域 API 保持可读且不需要机械重写。真正的类型所有权位于更低层、无业务逻辑和无持久化依赖的 identity 包。

CoreQuery 的 DTO、`GetTask`、`GetPlan`、`LoadPlanDocument` 及内部 Plan Step 查询使用这些强类型 ID。ApprovalSubmission 的 Plan loader 接口接受 `kernel.PlanID`；Gateway 只在 HTTP path 解析完成后把字符串转换为对应领域 ID，后续应用调用不再使用可互换字符串。

这些类型的底层表示仍为 string，JSON 编码保持协议兼容。格式、非空和归属校验仍由对应领域构造器或应用用例负责；identity 包不发展成通用验证框架、ID 生成器或全局实体模型。

## 边界规则

- `coreidentity` 不导入领域、存储、传输或可选模块包；
- Core 内部包可以依赖 `coreidentity`；
- `pkg/*api` 公共协议不得导入 `internal/coreidentity`，跨进程协议继续声明自己的字符串字段并在适配边缘转换；
- HTTP、JSON 与 SQL 驱动边缘可以接触字符串，进入应用/领域接口后应尽快转换为具体 ID 类型；
- 不创建可互换的通用 `ID` 类型。

## 后果

- Task、Plan、Step、Run、Approval 与 Grant 标识在 Core 内获得编译期区分；
- CoreQuery 无需依赖 Approval 仓储包即可表达 Approval ID；
- Kernel 与 Approval 的现有 API 名称和 JSON 线格式保持兼容；
- 新增 Core 用例应复用具体标识类型，而不是重新引入裸字符串；
- 可选模块协议仍保持进程边界隔离，不因内部类型统一而耦合到 Core 实现。

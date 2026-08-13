# ADR 029：UTC 时间存储与展示边界

## 状态

Accepted，2026-07-16。

## 背景

Core 的 Task、Plan、Step、Run、Approval、Capability Grant 与 Event 都跨越领域、持久化、应用查询和传输边界。若不同边界保留调用方时区或依赖进程级 `time.Local`，同一时刻可能出现多种字符串表示，排序、过期判断、测试和客户端展示也会逐渐耦合。

当前项目是本地优先系统，尚不需要通用时区框架。需要的是一个最小且可验证的契约：持久化与服务端协议使用统一时间基准，展示层再根据用户上下文转换。

## 决策

### 存储与领域时间

- Core SQLite 中的新增时间值统一写为 UTC 的 RFC3339Nano 字符串；
- 领域构造和命令输入在进入持久化前规范化为 UTC；
- Kernel aggregate、Capability Grant 与 Event 从存储读取后返回 `time.Time` 的 UTC 表示；
- Approval 的决定时间和可选过期时间在比较、存储和返回前统一转换为 UTC，避免返回对象继续持有调用方时区；
- 不修改事件顺序语义：事件增量读取和重放仍以全局 `sequence` 或 aggregate version 为准，不以墙上时钟排序。

### 查询与传输时间

`internal/corequery.Store` 对 Task、Plan、Plan Step、Approval 与 Run DTO 中的时间字段执行 RFC3339Nano 解析，并输出规范 UTC 字符串。Gateway 继续直接序列化这些 DTO，因此本地 HTTP API 的时间字段保持单一、确定的 UTC 表示。

为兼容已有的合法数据，查询和 Repository 读取允许 RFC3339Nano 中携带非零 offset 的时间，并在返回前转换为 UTC。无法解析的持久化时间不做猜测或静默替换，而是返回错误并 fail closed。

### 展示职责

CLI、GUI 或其他客户端在展示时根据用户选择或操作系统上下文转换时区。服务端不设置进程全局 `time.Local`，不在 DTO 中附加隐式本地时间，也不引入通用 timezone service。未来只有在出现跨用户时区偏好、日历规则或本地时间调度等真实用例时，才新增明确的应用契约。

## 边界规则

- 存储格式：`time.UTC().Format(time.RFC3339Nano)`；
- 读取格式：严格使用 `time.Parse(time.RFC3339Nano, value)`，成功后转换为 UTC；
- API 格式：RFC3339Nano UTC 字符串；
- 展示转换：由 CLI/UI/client 负责；
- 排序与并发：使用 sequence、version 或明确的数据库排序键，不依赖本地时区；
- 非法持久化时间：返回带上下文的错误，不使用当前时间兜底。

## 后果

- 同一时刻在 Core 存储和 API 中只有一个规范表示，测试和诊断结果稳定；
- 调用方仍可提交任意合法 offset 的 `time.Time`，但进入 Core 后立即规范化；
- 旧的合法 offset 时间可继续读取并被规范输出；
- 客户端展示职责更加明确，但当前服务端不会替客户端选择时区；
- 该决策没有引入全局 clock、timezone provider 或额外框架，后续真实需求可在应用层按用例扩展。

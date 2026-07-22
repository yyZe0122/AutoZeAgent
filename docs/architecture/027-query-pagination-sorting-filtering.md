# ADR 027：查询分页、排序与过滤契约

## 状态

Accepted，2026-07-16。

## 背景

Core 的 Task、Plan、Approval 与 Run 列表最初只接受一个 `limit` 整数，并固定按最新记录倒序返回。该形式无法表达翻页和受控过滤；若 Gateway 自行拼装查询参数或直接拥有 SQL，又会破坏 ADR 022 建立的应用查询边界。

本地管理读取的数据规模有限，事件流已经拥有基于 sequence 的增量读取，因此没有必要为普通资源列表引入游标编码、通用查询语言或 ORM。

## 决策

### 应用查询类型

`internal/corequery` 定义并拥有：

- `Page{Limit, Offset}`，其中 `Limit` 必须在 1 到 500 之间，`Offset` 不得为负数；
- `SortDirection`，只允许 `asc` 或 `desc`；
- `TaskListOptions`、`PlanListOptions`、`ApprovalListOptions` 与 `RunListOptions`；
- Task、Plan、Run 只开放 `State` 过滤，Approval 只开放 `Decision` 过滤。空过滤值表示不过滤。

这些是资源专用契约，不引入通用 map、任意字段表达式或可扩展 SQL DSL。新增过滤条件必须先成为明确的应用查询字段。

### 排序与分页

普通资源列表采用 offset 分页。默认由 Gateway 提供 `limit=100`、`offset=0`、`order=desc`。每个查询使用稳定的双字段顺序：业务时间字段在前，资源 ID 在后，并让两个字段使用相同方向，以便时间相同时仍有确定结果。

Gateway 接受：

- 通用参数：`limit`、`offset`、`order=asc|desc`；
- Task、Plan、Run：`state`；
- Approval：`decision`。

列表响应增加 `page` 对象，返回 `limit`、`offset`、本页 `returned` 数量和实际 `order`。这不是总数统计接口；当前不额外执行 `COUNT(*)`。

### SQL 边界

表名、列名、过滤字段和排序字段全部固定在 `corequery.Store` 中。过滤值、limit 和 offset 使用 SQL 参数绑定。排序方向只有在应用层枚举验证通过后才转换为固定的 `ASC` 或 `DESC` 片段，不接受任意客户端 SQL 标识符。

### 与事件读取的区别

`/v1/events` 与 SSE 事件流继续使用 `after` sequence。sequence 是 append-only 事件日志的天然游标，可支持增量消费；普通资源列表的 offset 分页只用于本地管理读取，不用于事件重放，也不承诺在并发写入下形成不可变快照。

## 后果

- Gateway、测试替身和后续传输适配器依赖同一组类型化查询契约；
- Core SQL 仍被限制在读模型内，传输层只解析协议参数；
- 页大小有明确上限，排序有稳定 ID tie-break，避免无界读取和同时间记录顺序漂移；
- offset 分页实现简单，符合当前本地数据库场景；若未来出现高基数远程查询，应基于实际性能证据另立 ADR，而不是提前引入游标框架；
- 不返回总数可避免每次列表请求附带额外全表计数成本。

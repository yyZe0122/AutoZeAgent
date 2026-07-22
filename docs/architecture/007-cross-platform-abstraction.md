# ADR-007：跨平台抽象

- 状态：Accepted
- 日期：2026-07-13

## 决策

Windows 作为当前开发和测试平台，Linux 作为首要部署平台。平台差异集中在 `internal/platform`，通过 Go build tags 实现 paths、transport、service、signals、process 和 sandbox。

业务层不得直接判断 GOOS。CI 至少执行 Windows 本地测试和 Linux amd64/arm64 交叉编译。

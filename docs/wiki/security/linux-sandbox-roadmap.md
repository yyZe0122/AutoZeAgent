# Linux Tool Sandbox Roadmap

- 状态：Roadmap
- 日期：2026-07-13
- 适用范围：Linux 本地服务器上的 `process_exec` 与 Git 子进程

## 当前基线

Phase 5 已实现以下跨平台基线：

- Tool Broker 是内置工具的唯一组合入口。
- Linux/Unix 子进程使用独立 process group，取消和超时终止整个进程组。
- Windows 使用 Job Object 终止进程树。
- stdout/stderr 分别限流。
- 工作目录限定在配置 root 内，并解析 symlink 后检查 containment。
- 环境变量采用 allowlist。
- Linux Runner 支持配置 UID/GID。
- 命令和参数不经过 shell 拼接。

这些措施能防止常见误执行和残留子进程，但还不是抵御恶意本机代码的完整沙箱。

## 第一阶段：systemd 与 cgroups v2

**状态（P5b）**：已实现 **process isolation baseline**（`internal/tools/internal/executor`）。实现方式为 `systemd-run --scope`（优先 `--user`，否则 system）；非 Linux 或探测失败显式降级。

生产服务优先由 systemd 管理。每次高风险工具执行可进入 transient scope。已落地属性：

```text
MemoryMax=          (default 512M)
MemorySwapMax=      (default 0)
CPUQuota=           (default 200%)
TasksMax=           (default 256)
RuntimeMaxSec=      (from Broker deadline + 5s when present)
```

路线图中其余属性（I/O 带宽、`NoNewPrivileges` / `PrivateTmp` / `ProtectSystem` 等）主要面向 **service** 沙箱，不硬塞进 scope 以免假安全感；完整文件系统隔离见第二阶段及以后。

实施要求（已对齐代码）：

- 检测宿主机是否使用 cgroups v2 unified hierarchy（`/sys/fs/cgroup/cgroup.controllers`）。
- 无 systemd / 无 `systemd-run` / probe 失败必须明确降级并记录 Audit（`process.isolation`），不能静默声称资源限制已启用。
- Broker 超时仍是上层最后期限，cgroup/systemd 是额外防线。
- transient unit 名称绑定 Tool Call ID（`ymz-tool-<id>.scope`），`--collect` + best-effort `systemctl stop` 清理。

## 第二阶段：namespace 隔离

评估为工具进程创建以下 namespace：

- user namespace：映射非特权 UID/GID。
- mount namespace：只读根文件系统，仅 bind mount 获批 workspace。
- PID namespace：限制进程视图。
- network namespace：默认无网络；只有网络类工具显式启用。
- IPC/UTS namespace：减少与宿主共享状态。

工作目录授权应转换成最小 bind mount 集，而不是把整个宿主文件系统暴露给子进程。

## 第三阶段：bubblewrap

优先评估 bubblewrap 作为可部署的 namespace 封装层：

```text
bwrap
  --ro-bind /usr /usr
  --ro-bind /bin /bin
  --bind <approved-workspace> /workspace
  --tmpfs /tmp
  --chdir /workspace
  --unshare-pid
  --unshare-ipc
  --unshare-uts
  --die-with-parent
```

网络默认使用 `--unshare-net`。只有经过审批且确有需要的工具才能使用受控网络配置。实际参数必须由类型化代码生成，禁止把 Planner 文本直接拼入 bubblewrap 命令。

## 第四阶段：seccomp

在实际工具集和目标发行版上采集系统调用后，为不同工具族建立最小 syscall profile：

- 文件读取工具。
- 文件写入/补丁工具。
- 编译与测试进程。
- Git 本地操作。
- 网络客户端。

默认拒绝高风险系统调用，例如新的 namespace 创建、mount、ptrace、内核模块、原始 socket 和 keyring 操作。profile 版本必须进入审计和部署清单。

## 第五阶段：网络与 SSRF 加固

`http.get` 后续应增加：

- 域名解析结果的 IP 分类。
- 默认拒绝 loopback、link-local、multicast、metadata service 和未批准私网段。
- 连接前后复核目标地址，降低 DNS rebinding 风险。
- 可选的固定出口代理和域名 allowlist。
- 响应压缩解码后的大小上限、连接超时和 header 上限。

## 验收标准

只有满足以下条件，才能把 Linux 执行环境标记为“OS sandbox enabled”：

- 资源限制可通过测试触发，并在超限后完整清理进程树。
- workspace 外文件不可读写。
- 默认无网络，获批网络策略可验证。
- sandbox 不可用时 Fail Closed，或由管理员显式选择并审计降级模式。
- 重启后不存在遗留 transient unit、cgroup 或子进程。
- amd64 与 arm64 的目标发行版集成测试通过。

在以上验收完成前，文档和 UI 必须使用“process isolation baseline”，不得宣称当前实现是完整安全沙箱。

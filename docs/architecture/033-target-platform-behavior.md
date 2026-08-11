# 033 目标平台行为与兼容边界

## 状态

已接受，2026-07-17。

## 背景

YunmengZe 的正式目标平台是 Windows AMD64 与 Linux AMD64。Core、Gateway、Tool Executor 和文件写入链路已经存在平台实现，但路径目录、进程退出、信号、文件替换、Unix Socket、systemd 和 SQLite 错误分类需要形成同一套可验收边界，避免调用方依赖操作系统细节或驱动错误文本。

当前没有需要持久化的应用级 CacheDir。Artifact、Gateway 和文件写入使用的临时文件必须与目标文件位于同一目录，以保持同文件系统替换语义，并在成功或失败后清理。

## 决策

### 平台差异隔离

平台差异继续限制在小型 build-tag 文件中：

- 路径解析使用 `*_windows.go` 与 `*_linux.go`；
- 本地 Endpoint 在 Windows 使用 loopback TCP 与随机 Token，在 Linux 使用权限受限的 Unix Socket；
- Tool Executor 在 Windows 使用 Job Object，在 Linux 使用进程组终止子进程树；
- 关停信号在 Windows 使用 `os.Interrupt`，在 Linux 同时处理 `os.Interrupt` 与 `SIGTERM`；
- 文件替换在 Unix 使用原子 rename，在 Windows 使用 remove + rename 兼容替代。

调用方只依赖公共接口，不根据 `runtime.GOOS`、平台错误字符串或 Endpoint 传输细节分支。

### 目录语义

Windows 用户模式使用 `APPDATA\YunmengZe` 作为配置目录，使用 `LOCALAPPDATA\YunmengZe` 作为数据根目录；系统模式使用 `ProgramData\YunmengZe`。Linux 用户模式遵循 `XDG_CONFIG_HOME`、`XDG_DATA_HOME` 和 `XDG_RUNTIME_DIR`，缺失时回退到用户主目录下的 XDG 默认位置；系统模式使用 `/etc/yunmengze`、`/var/lib/yunmengze`、`/run/yunmengze` 和 `/var/log/yunmengze`。

YunmengZe 不新增无调用方的持久化 CacheDir。临时文件由具体写入者拥有，优先在目标目录创建，以保证替换不跨文件系统。

### Linux 服务与权限

正式 systemd 单元是 `packaging/systemd/yunmengze.service`。验收至少校验专用用户和组、control-group 终止、Runtime/State 目录、私有临时目录与 NoNewPrivileges，并在可用时通过 `systemd-analyze verify` 检查单元语法。非 Linux 或未安装 systemd 工具的环境显式跳过该项，不伪装成 systemd 实机验收。

Unix Socket 必须拒绝已有的非 Socket 路径，限制 Socket 和父目录权限，并只清理当前进程拥有的 Endpoint。

### 稳定错误分类

SQLite UNIQUE 与 PRIMARY KEY 冲突通过公共 `pkg/sqliteerror` 中基于驱动稳定扩展结果码的分类器处理，不解析 `error.Error()` 文本。应用层继续将冲突映射到既有领域错误；未知数据库错误保留原始错误链。

## 验收

- Windows AMD64：验证目录语义、真实 PowerShell 的工作目录/环境/退出码、Interrupt 关停、loopback Endpoint、文件替换和 Job Object 子进程树退出；
- Linux AMD64：验证 XDG/系统目录、真实 `/bin/sh`、`SIGTERM`、Unix Socket 权限与符号链接边界、文件替换、进程组退出和 systemd 单元；
- 两个平台均运行全量测试、依赖校验和无 CGo 构建；关键安全边界在 Linux 运行 Race Detector。

## 后果

- 平台行为由测试和正式打包文件共同定义，而不是依赖开发机约定；
- 调用方不再通过 SQLite 错误字符串判断 UNIQUE 冲突；
- CacheDir 不会在没有业务用途时扩展公共路径模型；
- systemd 静态检查可进入 Linux CI，但真正的安装、启动、停止和权限验证仍属于 Linux 主机发布验收。

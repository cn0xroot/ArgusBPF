# Unix-Monitor

*[English](README.md)*

**实时查看操作系统本身在做什么——进程、文件、网络、内存、磁盘、内核活动——用一个会"说人话"的仪表盘，而不只是一堆 syscall 术语。**

Unix-Monitor 是一个单文件 Go 二进制程序。在 Linux/amd64 上以 root 运行时，它通过 **eBPF**（CO-RE，运行时无需内核头文件）直接挂钩内核；在其它平台（其它系统、其它架构、无 root）会自动降级为基于 [gopsutil](https://github.com/shirou/gopsutil) 的跨平台轮询采集。无论哪种方式，你都能在 Web UI 里随时切换**专业模式**（syscall 原始形态、内存地址、扇区号、原始字段）和**通俗模式**（同一个事件，换成日常语言+生活类比来解释）。

思路上和 CC-Monitor 一类监控 AI 编码助手对电脑做了什么的工具类似——只是这次监控对象是操作系统本身。

## 功能

- **实时事件流**：exec、文件 open/write/unlink/rename、网络 connect/listen、mmap/mprotect/ptrace/跨进程内存访问、内核模块加载、磁盘块 IO，通过 WebSocket 实时推送。
- **风险规则**：一份小巧的 JSON 规则集（可编辑、可被用户覆盖），标记诸如读取 `/etc/shadow`、可写+可执行内存页、`ptrace` 附加、跨进程内存写入、内核模块加载、SSH 活动等，分为 info/low/medium/high 四档。
- **通俗解释**：每个事件都同时拥有一句专业描述（`mprotect(addr=0x7f.., prot=RWX)`）和一句带生活类比的大白话解释，由离线的模板/查表引擎生成（不调用任何 LLM，完全确定性）。
- **eCapture 风格的应用层探针**：在 `getaddrinfo`（DNS）、OpenSSL 的 `SSL_read`/`SSL_write`（TLS 明文）、Postgres 的 `exec_simple_query`（SQL 语句）上按需挂载 uprobe；目标库/程序存在时自动挂载，不存在时优雅跳过并在 `/api/info` 中给出提示。
- **完整的 Web 仪表盘**：总览页（实时 CPU/内存/磁盘/网络曲线）、实时事件表、泳道式时间线、网络连接、磁盘 IO（含扇区级散点图）、按进程的内存地图（可视化并逐段解释 `/proc/<pid>/maps`）、进程树、告警页、术语知识库——视觉风格参考 Grafana（深色面板网格、图例、时间范围选择器），但完全自包含，不需要安装真正的 Grafana。
- **天生跨平台**：纯 Go，无 CGO（SQLite 使用 `modernc.org/sqlite`）。可编译到 darwin/windows/linux 的 amd64/arm64；只有 eBPF 采集器是 linux/amd64 专属的，其余部分到处都能跑。

## 快速开始

```sh
# 编译（不需要 clang/bpftool —— eBPF 对象文件已预编译好并提交到仓库）
make build

# 以 root 运行才能用 eBPF 采集；无 root 会自动降级为轮询模式
sudo ./unix-monitor

# 打开仪表盘
open http://127.0.0.1:9900
```

常用参数：

| 参数 | 默认值 | 含义 |
|---|---|---|
| `--listen` | `127.0.0.1:9900` | HTTP 监听地址 |
| `--token` | *(无)* | 要求请求带 `X-Token` 头 / `?token=` 参数 |
| `--db` | `~/.unix-monitor/events.db` | SQLite 数据库路径 |
| `--rules` | `~/.unix-monitor/rules.json` | 覆盖内置风险规则集 |

## 从源码编译 eBPF 程序

仓库里已经提交了预编译好的 `internal/collector/monitor_x86_bpfel.o`，所以 `make build` 不需要 clang。如果你修改了 `bpf/monitor.c`：

```sh
make vmlinux   # 导出本机内核的 BTF -> bpf/vmlinux.h（需要 bpftool）
make bpf       # 重新编译 monitor.c -> .o/.go 文件对（需要 clang、llvm-strip）
make build
```

## 架构

完整设计（事件模型、采集管线、风险规则、专业/通俗双解释引擎、`web/` 所用的 HTTP/WebSocket API 契约）见 [DESIGN.md](DESIGN.md)。

```
采集器 (eBPF | 轮询) -> 管线 (规则 + 解释 + 1秒聚合) -> SQLite + WebSocket Hub -> Web UI
```

## 平台支持

| 系统/架构 | 采集方式 | 说明 |
|---|---|---|
| Linux/amd64，root | eBPF (CO-RE) | 细节最全：syscall 参数、内存地址、扇区号 |
| Linux/amd64，非 root | 轮询 | 自动降级 |
| Linux/arm64 | 轮询 | eBPF 的 syscall 号表目前只做了 amd64 |
| macOS、Windows | 轮询 | 基于 gopsutil 的进程/连接差分 |

## 已知局限

- 未实现 MySQL `dispatch_command` 的 SQL 捕获——其参数结构随版本变化太大，风险高，暂不提供；Postgres 的 `exec_simple_query`（单个 `char*` 参数，签名稳定）已实现。
- 暂无 `bash readline` 命令审计 uprobe。
- eBPF 路径暂未单独捕获入站连接（`accept`）；磁盘页的"按文件统计 IO"暂无数据源（目前只有按进程统计和扇区散点图）。
- `install.sh` 目前只负责放置二进制文件并可选运行一次，尚未安装 systemd 服务。

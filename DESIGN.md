# Unix-Monitor 技术方案

> 目标：参考 CC-Monitor（监控 Claude Code 对电脑的操作），做一个监控**操作系统本身所有活动**的工具 ——
> 进程、文件、网络、内存、磁盘、内核/安全事件；提供 Web UI，并同时提供
> **专业模式**（syscall 参数、内存地址、扇区号、fd、flags…）和**通俗模式**（小白能看懂的类比解释）。

## 1. 技术选型

| 项 | 选择 | 理由 |
|---|---|---|
| 语言 | Go 1.26 | 单二进制、交叉编译方便、cilium/ebpf 生态成熟 |
| Linux 内核采集 | eBPF（cilium/ebpf v0.22，CO-RE，tracepoint/kprobe/uprobe，ringbuf） | 零侵入、低开销、可拿到内核级细节（地址、扇区、返回值） |
| 跨平台采集 | gopsutil v4 轮询（Linux / macOS / Windows / FreeBSD） | 无 root / 无 eBPF 时自动降级 |
| 系统指标 | Linux: /proc；其他: gopsutil | CPU、内存、磁盘、网卡吞吐曲线 |
| 存储 | 内存环形缓冲（实时）+ SQLite（modernc 纯 Go，无 CGO） | 无 CGO 便于交叉编译；历史可查询 |
| 推送 | WebSocket（改进 CC-Monitor 的 REST 轮询） | 事件实时到达，批量合包 |
| 前端 | 原生 JS + Canvas，`go:embed` 打进二进制 | 与 CC-Monitor 一致：无构建链、单文件分发 |

## 2. 架构

```
 ┌──────────────── 采集层 Collector（按平台/权限自动选择）────────────────┐
 │ eBPF(Linux,root)    : exec/exit/fork, openat, read/write 聚合, unlink,   │
 │                       rename, connect/accept/bind/listen, DNS(uprobe),   │
 │                       tcp 收发字节, mmap/mprotect/munmap/brk,            │
 │                       ptrace, process_vm_readv/writev, 缺页采样,         │
 │                       块设备请求(扇区), 内核模块加载, setuid, kill       │
 │ Poller(全平台)      : 进程新建/退出差分、连接差分、进程 IO/内存差分     │
 │ SysStats            : CPU/内存/磁盘/网卡 每秒采样                        │
 └───────────────┬─────────────────────────────────────────────────────────┘
                 ▼  RawEvent
 ┌──────── 处理管线 Pipeline ────────┐
 │ 1. Enrich  : 补进程名/exe/用户/fd→路径 │
 │ 2. Aggregate: 高频事件(读写/缺页/块IO)按 (pid,类型,目标) 1s 合并 │
 │ 3. Rules   : 风险规则(JSON, 可自定义) → risk/rule/告警标题      │
 │ 4. Explain : 生成 pro 摘要 + plain 通俗解释 + 类比              │
 └───────────────┬──────────────────┘
                 ▼  Event
     Ring buffer ── SQLite ── WebSocket Hub ── HTTP API ── Web UI
```

## 3. 事件模型

```jsonc
{
  "id": 123, "ts": 1759300000123,          // 毫秒
  "cat": "file|process|net|memory|disk|kernel|security",
  "type": "open|read|write|exec|connect|mmap|mprotect|ptrace|vm_read|block_io|...",
  "pid": 1234, "ppid": 1, "tid": 1234, "uid": 0, "user": "root",
  "comm": "curl", "exe": "/usr/bin/curl",
  "risk": "info|low|medium|high", "rule": "mem.wx_page", "rule_title": "内存同时可写可执行",
  "title": "openat",                        // 专业短标题
  "pro":   "openat(\"/etc/passwd\", O_RDONLY) = 3",
  "plain": "程序 curl 打开了系统账户清单文件来查看内容",
  "analogy": "就像翻开了小区的住户登记簿",
  "fields": { "path": "/etc/passwd", "flags": "O_RDONLY", "fd": 3, "ret": 3 },
  "count": 1                                // 聚合条数
}
```

## 4. 双模式解释引擎（internal/explain）

- **专业模式**：还原 syscall 形态 `mmap(0x0, 4096, PROT_READ|PROT_WRITE, MAP_PRIVATE|MAP_ANONYMOUS, -1, 0) = 0x7f3a2c000000`，
  显示 fd、flags 解码、地址、长度、扇区号、errno 等。
- **通俗模式**：模板 + 知识库，不依赖 LLM，离线可用：
  - 路径语义识别：`/etc/shadow` → "系统密码库"，`~/.ssh` → "SSH 钥匙"，`/proc/<pid>/mem` → "另一个程序的内存"…
  - 进程语义识别：浏览器、包管理器、shell、编译器…
  - 端口语义：443 → "加密网页(HTTPS)"、22 → "远程登录(SSH)"、53 → "查域名(DNS)"…
  - 每类事件带一个类比（"mmap 像向仓库申请一块货架"）。
  - 知识库页面：解释 syscall、fd、页、缺页、扇区、TCP 握手等概念。

## 5. 风险规则（internal/rules，`default_rules.json`，可放 `~/.unix-monitor/rules.json` 覆盖）

字段与 CC-Monitor 保持一致：`id, risk, types, field, pattern(正则), title, desc`。示例：
- high：读写 `/etc/shadow`、`/proc/*/mem`；ptrace 附加；process_vm_writev；mprotect 产生 W+X 页；加载内核模块；`/tmp` 下的可执行文件被执行
- medium：写 `/etc/*`、修改 `~/.bashrc`/crontab、连接非常见端口、setuid
- low：外部网络连接、监听端口

## 5b. 应用层明文采集（借鉴 gojue/ecapture，uprobe）

用 eBPF uprobe 挂在用户态库/程序的函数上，拿到**加密前/解密后的明文**与高层语义，
不做中间人、不改证书。属于 `cat:"security"` 或 `net`，默认 medium 风险、可配脱敏。

| 模块 | 挂载点 | 捕获内容 | 开关 |
|---|---|---|---|
| TLS 明文 | `libssl` `SSL_write`/`SSL_read`（OpenSSL/BoringSSL），GnuTLS `gnutls_record_send/recv`，NSS | HTTPS/TLS 明文收发（截断预览，默认脱敏） | `--tls` |
| Go TLS | Go 程序 `crypto/tls (*Conn).Write/Read`（按符号/偏移） | 纯 Go 程序的 TLS 明文 | `--tls` |
| Bash 审计 | bash `readline` 返回处 uprobe | 交互式 shell 实际执行的命令行 | `--bash` |
| MySQL | `mysqld` `dispatch_command` | SQL 查询语句 | `--db` |
| PostgreSQL | `postgres` `exec_simple_query` | SQL 查询语句 | `--db` |
| SSH | sshd/ssh 连接事件（结合 connect/accept + 端口22 + comm） | 登录来源、方向 | 默认开 |

- 库定位：解析目标进程 `/proc/pid/maps` 找到 `libssl.so` 路径与基址，`--tls` 时对新出现的
  使用 libssl 的进程自动 attach；也可 `--tls-pid <pid>` 指定。
- 事件新增字段：`fields.payload`（明文预览，≤512B，可 `--no-payload` 关闭）、`fields.sql`、`fields.cmdline`、`fields.tls_version`。
- UI：网络页新增「明文/协议」子表；专业模式显示十六进制+ASCII，通俗模式显示"某程序通过加密通道发送了这些内容"。

## 6. Web UI 页面

| 页面 | 内容 |
|---|---|
| 总览 | CPU/内存/磁盘/网络实时曲线、分类计数卡片、风险分布、Top 进程 |
| 实时事件 | 事件流（筛选：分类/风险/进程/关键词，暂停/自动滚动），点击看详情 |
| 时间线 | 泳道图（按分类或按进程），风险着色，可选 5m/1h/6h/24h，点击下钻 |
| 网络 | 当前连接表（进程、远端、域名、状态、端口含义）、DNS 查询、流量 Top |
| 磁盘 | 设备吞吐、块请求扇区散点图、文件读写 Top |
| 内存 | 系统内存构成、进程内存地图（/proc/pid/maps 可视化 + 区域通俗标注）、mmap/mprotect 事件 |
| 进程 | 进程树、详情（命令行、fd、内存、IO、连接） |
| 告警 | 中/高风险事件、命中规则 |
| 知识库 | 名词解释（通俗） |

顶栏全局开关：**专业 / 通俗** 模式切换（影响表格列、详情、标题）、深/浅主题、采集后端状态。

## 7. HTTP / WebSocket API

| 接口 | 说明 |
|---|---|
| `GET /api/info` | 版本、主机、OS、采集后端（ebpf/poll）、能力列表 |
| `GET /api/events?cat=&risk=&type=&pid=&q=&before=&limit=` | 历史事件（SQLite） |
| `GET /api/stats` | 分类/风险/类型计数、Top 进程、每秒速率 |
| `GET /api/timeline?range=3600&lane=cat` | 时间桶聚合 |
| `GET /api/system` / `GET /api/system/history` | 当前系统快照 / 最近 10 分钟曲线 |
| `GET /api/processes` | 进程列表 |
| `GET /api/process/{pid}` | 进程详情 + 内存地图 + fd + 连接 |
| `GET /api/connections` | 当前 socket |
| `GET /api/disk` | 设备统计 + 文件 IO Top |
| `GET /api/rules` / `GET /api/glossary` | 规则 / 知识库 |
| `WS /ws` | 推送 `{"t":"events","d":[Event...]}`（≤200ms 合包）与 `{"t":"sys","d":Snapshot}`（1s） |

## 8. 跨平台策略

- `collector_linux.go`：有 root + BTF → eBPF；否则降级 Poller。
- `collector_other.go`（darwin/windows/freebsd）：Poller（gopsutil）。
- 纯 Go 无 CGO：`GOOS=darwin/windows go build` 可直接交叉编译；eBPF 字节码预编译后 `go:embed`。

## 9. 安全 / 性能

- 默认只监听 `127.0.0.1:9900`，`--listen` 可改；可选 `--token` 访问令牌。
- 过滤自身 PID 防止反馈环；read/write/缺页/块 IO 内核侧计数 + 用户态 1s 合并，避免事件风暴。
- SQLite 批量事务写入，按条数/天数自动清理（默认 50 万条 / 7 天）。

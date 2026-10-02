# ArgusBPF — Technical Design

*[中文说明](DESIGN.zh-CN.md)*

> Goal: following CC-Monitor's approach (which watches what Claude Code does to a machine), build a tool that watches **everything the operating system itself does** —
> processes, files, network, memory, disk, kernel/security events — with a Web UI that offers both
> a **Professional mode** (syscall args, memory addresses, sector numbers, fd, flags…) and a **Plain-language mode** (the same technical mechanics explained through everyday analogies).

## 1. Technology choices

| Item | Choice | Rationale |
|---|---|---|
| Language | Go 1.26 | Single binary, easy cross-compilation, mature cilium/ebpf ecosystem |
| Linux kernel collection | eBPF (cilium/ebpf v0.22, CO-RE, tracepoint/kprobe/uprobe, ringbuf) | Non-intrusive, low overhead, kernel-level detail (addresses, sectors, return values) |
| Cross-platform collection | gopsutil v4 polling (Linux / macOS / Windows / FreeBSD) | Automatic fallback with no root / no eBPF |
| System metrics | Linux: /proc; elsewhere: gopsutil | CPU, memory, disk, NIC throughput curves |
| Storage | In-memory ring buffer (live) + SQLite (modernc, pure Go, no CGO) | No CGO keeps cross-compilation easy; history stays queryable |
| Push | WebSocket (an improvement over CC-Monitor's REST polling) | Events arrive live, batched |
| Frontend | Vanilla JS + Canvas, embedded into the binary via `go:embed` | Matches CC-Monitor: no build chain, single-file distribution |

## 2. Architecture

```
 ┌──────────────── Collector layer (auto-selected by platform/privilege) ──┐
 │ eBPF (Linux, root): exec/exit/fork, openat, read/write aggregation,     │
 │                      unlink, rename, connect/accept/bind/listen,        │
 │                      DNS (uprobe), tcp bytes sent/received,             │
 │                      mmap/mprotect/munmap/brk, ptrace,                  │
 │                      process_vm_readv/writev, page-fault sampling,      │
 │                      block device requests (sector), kernel module      │
 │                      load, setuid, kill                                 │
 │ Poller (all platforms): new/exited process diff, connection diff,       │
 │                      per-process IO/memory diff                         │
 │ SysStats           : CPU/memory/disk/NIC sampled every second           │
 └───────────────┬───────────────────────────────────────────────────────────┘
                 ▼  RawEvent
 ┌──────── Pipeline ────────┐
 │ 1. Enrich  : fill in process name/exe/user/fd→path                     │
 │ 2. Aggregate: fold high-frequency events (read/write/pagefault/block    │
 │               IO) per (pid, type, target) into 1s windows              │
 │ 3. Rules   : risk rules (JSON, user-customizable) -> risk/rule/alert    │
 │              title                                                      │
 │ 4. Explain : generate the pro one-liner + plain-language explanation    │
 │              + analogy                                                  │
 └───────────────┬──────────────────┘
                 ▼  Event
     Ring buffer ── SQLite ── WebSocket Hub ── HTTP API ── Web UI
```

## 3. Event model

```jsonc
{
  "id": 123, "ts": 1759300000123,          // milliseconds
  "cat": "file|process|net|memory|disk|kernel|security",
  "type": "open|read|write|exec|connect|mmap|mprotect|ptrace|vm_read|block_io|...",
  "pid": 1234, "ppid": 1, "tid": 1234, "uid": 0, "user": "root",
  "comm": "curl", "exe": "/usr/bin/curl",
  "risk": "info|low|medium|high", "rule": "mem.wx_page", "rule_title": "Memory page became writable+executable",
  "title": "openat",                        // short professional label
  "pro":   "openat(\"/etc/passwd\", O_RDONLY) = 3",
  "plain": "The program curl opened the system account list file to look at it",
  "analogy": "Like flipping open the building's resident registry",
  "fields": { "path": "/etc/passwd", "flags": "O_RDONLY", "fd": 3, "ret": 3 },
  "count": 1                                // number of aggregated raw events
}
```

## 4. Dual-mode explanation engine (internal/explain)

- **Professional mode**: reconstructs the syscall form, e.g. `mmap(0x0, 4096, PROT_READ|PROT_WRITE, MAP_PRIVATE|MAP_ANONYMOUS, -1, 0) = 0x7f3a2c000000`,
  showing fd, decoded flags, address, length, sector number, errno, etc.
- **Plain-language mode**: template + knowledge base, no LLM dependency, works fully offline:
  - Path semantics: `/etc/shadow` → "the system password vault", `~/.ssh` → "SSH keys", `/proc/<pid>/mem` → "another program's memory"…
  - Process semantics: browsers, package managers, shells, compilers…
  - Port semantics: 443 → "encrypted web (HTTPS)", 22 → "remote login (SSH)", 53 → "domain lookup (DNS)"…
  - Every event category carries an analogy ("mmap is like requesting a shelf of warehouse space").
  - A glossary page explains concepts like syscalls, fds, pages, page faults, sectors, the TCP handshake, etc.

## 5. Risk rules (internal/rules, `default_rules.json`, overridable via `~/.argusbpf/rules.json`)

Fields follow the same shape CC-Monitor uses: `id, risk, types, field, pattern (regex), title, desc`. Examples:
- high: reading/writing `/etc/shadow`, `/proc/*/mem`; a ptrace attach; process_vm_writev; mprotect producing a W+X page; loading a kernel module; executing a binary under `/tmp`
- medium: writing `/etc/*`, modifying `~/.bashrc`/crontab, connecting to an uncommon port, setuid
- low: an outbound network connection, a listening port

## 5b. Application-layer plaintext capture (borrowing from gojue/ecapture, uprobes)

eBPF uprobes hang off functions in user-space libraries/programs to recover **plaintext before encryption / after decryption** along with higher-level semantics,
with no man-in-the-middle proxy and no certificate tampering. These land under `cat:"security"` or `net`, default to medium risk, and can be configured to redact.

| Module | Attach point | Captures | Flag |
|---|---|---|---|
| TLS plaintext | `libssl`'s `SSL_write`/`SSL_read` (OpenSSL/BoringSSL), GnuTLS's `gnutls_record_send/recv`, NSS | HTTPS/TLS plaintext send/recv (truncated preview, redacted by default) | `--tls` |
| Go TLS | A Go program's `crypto/tls (*Conn).Write/Read` (by symbol/offset) | TLS plaintext for pure-Go programs | `--tls` |
| Bash audit | A uprobe at bash's `readline` return | The actual command line an interactive shell ran | `--bash` |
| MySQL | `mysqld`'s `dispatch_command` | SQL query text | `--db` |
| PostgreSQL | `postgres`'s `exec_simple_query` | SQL query text | `--db` |
| SSH | sshd/ssh connection events (combining connect/accept + port 22 + comm) | Login source, direction | on by default |

- Library discovery: parse the target process's `/proc/pid/maps` to find `libssl.so`'s path and base address; with `--tls`, newly-seen
  processes that use libssl are attached automatically, or a specific one can be named via `--tls-pid <pid>`.
- New event fields: `fields.payload` (plaintext preview, ≤512B, disable with `--no-payload`), `fields.sql`, `fields.cmdline`, `fields.tls_version`.
- UI: the network page gets a "plaintext/protocol" sub-table; professional mode shows hex+ASCII, plain-language mode shows "program X sent this content over an encrypted channel".

## 6. Web UI pages

| Page | Content |
|---|---|
| Overview | Live CPU/memory/disk/network curves, per-category count cards, risk distribution, top processes |
| Live events | Event stream (filter by category/risk/process/keyword, pause/autoscroll), click for detail |
| Timeline | Swimlane chart (by category or by process), risk-colored, 5m/1h/6h/24h ranges, click to drill in |
| Network | Current connection table (process, remote, domain, state, what the port means), DNS lookups, top traffic |
| Disk | Device throughput, block-request sector scatter plot, top file/process IO |
| Memory | System memory composition, per-process memory map (`/proc/pid/maps` visualized with plain-language region labels), mmap/mprotect events |
| Processes | Process tree, detail (command line, fds, memory, IO, connections) |
| Alerts | Medium/high-risk events, matched rules |
| Glossary | Plain-language term explanations |

Global toolbar toggles: **Professional / Plain-language** mode (affects table columns, detail, titles), dark/light theme, collector backend status.

## 7. HTTP / WebSocket API

| Endpoint | Description |
|---|---|
| `GET /api/info` | Version, host, OS, collector backend (ebpf/poll), capability list |
| `GET /api/events?cat=&risk=&type=&pid=&q=&before=&limit=` | Historical events (SQLite) |
| `GET /api/stats` | Per-category/risk/type counts, top processes, per-second rate |
| `GET /api/timeline?range=3600&lane=cat` | Time-bucketed aggregation |
| `GET /api/system` / `GET /api/system/history` | Current system snapshot / last 10 minutes of curves |
| `GET /api/processes` | Process list |
| `GET /api/process/{pid}` | Process detail + memory map + fds + connections |
| `GET /api/connections` | Current sockets |
| `GET /api/disk` | Device stats + top file IO |
| `GET /api/rules` / `GET /api/glossary` | Rules / glossary |
| `WS /ws` | Pushes `{"t":"events","d":[Event...]}` (batched, ≤200ms) and `{"t":"sys","d":Snapshot}` (1s) |

## 8. Cross-platform strategy

- `collector_linux.go`: root + BTF available → eBPF; otherwise falls back to the Poller.
- `collector_other.go` (darwin/windows/freebsd): the Poller (gopsutil).
- Pure Go, no CGO: `GOOS=darwin/windows go build` cross-compiles directly; the eBPF bytecode is precompiled and embedded via `go:embed`.

## 9. Security / performance

- Listens only on `127.0.0.1:1024` by default, changeable via `--listen`; an optional `--token` access token.
- Filters out its own PID to avoid a feedback loop; read/write/pagefault/block-IO counting happens kernel-side with a 1s user-space merge to avoid an event storm.
- SQLite writes happen in batched transactions, with automatic cleanup by row count/age (defaults: 500,000 rows / 7 days).

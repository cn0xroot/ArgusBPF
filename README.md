# Unix-Monitor

*[中文说明](README.zh-CN.md)*

**See everything your operating system is doing — processes, files, network, memory, disk, kernel activity — through a live dashboard that explains itself in plain language, not just syscall jargon.**

Unix-Monitor is a single Go binary. On Linux/amd64 running as root it hooks the kernel directly via **eBPF** (CO-RE, no kernel headers needed at runtime); everywhere else (other OSes, other architectures, no root) it falls back automatically to a cross-platform polling collector built on [gopsutil](https://github.com/shirou/gopsutil). Either way you get a web UI with a **Professional mode** (syscall forms, memory addresses, sector numbers, raw fields) and a **Plain-language mode** (short, everyday explanations and analogies for exactly the same events) that you can toggle at any time.

It's the same idea as tools like [CC-Monitor](https://github.com/) that watch what an AI coding agent does to your machine — applied to the operating system itself.

## Features

- **Live event feed** — every exec, file open/write/unlink/rename, network connect/listen, mmap/mprotect/ptrace/cross-process memory access, kernel module load, and block-device IO, streamed over WebSocket.
- **Risk rules** — a small JSON ruleset (editable, user-overridable) flags things like reading `/etc/shadow`, W+X memory pages, `ptrace` attach, cross-process memory writes, kernel module loads, SSH activity, and more, at info/low/medium/high severity.
- **Plain-language explanations** — every event gets both a technical one-liner (`mprotect(addr=0x7f.., prot=RWX)`) and a plain-English explanation with an everyday analogy, generated offline by a template/lookup engine (no LLM calls, fully deterministic).
- **eCapture-style application probes** — best-effort uprobes on `getaddrinfo` (DNS), OpenSSL `SSL_read`/`SSL_write` (TLS plaintext), and Postgres `exec_simple_query` (SQL text), attached automatically when the target library/binary is present; each one degrades gracefully (and says so in `/api/info`) if it isn't.
- **Full web dashboard** — overview with live CPU/memory/disk/network charts, live event table, swimlane timeline, network connections, disk IO (including a sector-level scatter plot), per-process memory maps (`/proc/<pid>/maps` visualised and explained region-by-region), a process tree, an alerts view, and a glossary of terms — styled like Grafana (dark panel grid, legends, time-range picker) but self-contained, no Grafana install required.
- **Cross-platform by design** — pure Go, no CGO (SQLite via `modernc.org/sqlite`). Builds for darwin/windows/linux on amd64/arm64; only the eBPF collector is linux/amd64-specific, everything else runs everywhere.

## Quick start

```sh
# Build (clang/bpftool not required — the eBPF object ships pre-built)
make build

# Run as root for the eBPF collector; without root it auto-falls-back to polling
sudo ./unix-monitor

# Open the dashboard
open http://127.0.0.1:9900
```

Useful flags:

| Flag | Default | Meaning |
|---|---|---|
| `--listen` | `127.0.0.1:9900` | HTTP listen address |
| `--token` | *(none)* | Require `X-Token` header / `?token=` query param |
| `--db` | `~/.unix-monitor/events.db` | SQLite database path |
| `--rules` | `~/.unix-monitor/rules.json` | Override the built-in risk ruleset |

## Building the eBPF program from source

A pre-built `internal/collector/monitor_x86_bpfel.o` is committed, so `make build` never needs clang. If you change `bpf/monitor.c`:

```sh
make vmlinux   # dump this kernel's BTF -> bpf/vmlinux.h (needs bpftool)
make bpf       # recompile monitor.c -> the .o/.go pair (needs clang, llvm-strip)
make build
```

## Architecture

See [DESIGN.md](DESIGN.md) (Chinese) for the full design: event model, collection pipeline, risk rules, the plain/professional explanation engine, and the HTTP/WebSocket API contract used by `web/`.

```
collector (eBPF | poller) -> pipeline (rules + explain + 1s aggregation) -> SQLite + WebSocket hub -> web UI
```

## Platform support

| OS / arch | Collector | Notes |
|---|---|---|
| Linux/amd64, root | eBPF (CO-RE) | full detail: syscall args, memory addresses, sector numbers |
| Linux/amd64, non-root | polling | automatic fallback |
| Linux/arm64 | polling | eBPF syscall-number table is amd64-specific for now |
| macOS, Windows | polling | process/network diffing via gopsutil |

## Known limitations

- MySQL `dispatch_command` SQL capture isn't implemented — its argument layout is too version-fragile to ship safely; Postgres (`exec_simple_query`, a stable single-`char*` signature) is.
- No `bash readline` command-audit uprobe yet.
- Inbound connections (`accept`) aren't individually captured by the eBPF path; the disk page's "top files by IO" has no data source yet (only per-process IO and the sector scatter plot).
- No systemd unit is installed by `install.sh` yet — it just places the binary and optionally runs it once.

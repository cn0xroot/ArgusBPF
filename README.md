# ArgusBPF

*[中文说明](README.zh-CN.md)*

**See what your OS — and every AI coding agent running on it — is actually doing, via eBPF, explained in plain language instead of syscall jargon.**

The name borrows from Argus, the many-eyed watchman of Greek myth, for what the tool actually does: hook the kernel directly (eBPF) while also recognizing when the activity it's watching belongs to an AI agent CLI specifically.

ArgusBPF is a single Go binary. On Linux/amd64 running as root it hooks the kernel directly via **eBPF** (CO-RE, no kernel headers needed at runtime); everywhere else (other OSes, other architectures, no root) it falls back automatically to a cross-platform polling collector built on [gopsutil](https://github.com/shirou/gopsutil). The web UI lets you toggle between **Professional mode** (syscall forms, memory addresses, sector numbers, raw fields) and **Plain-language mode** (the same events explained in everyday language with analogies).

It's the same idea as tools like [CC-Monitor](https://github.com/) that watch what an AI coding agent does to your machine, applied to the operating system itself, and extended to recognize which AI agent CLI is doing it.

## Design

- **A single kernel-side dispatcher, not per-process ptrace.** One CO-RE program (`bpf/monitor.c`) hangs off `raw_syscalls/sys_enter` plus the `module_load`/`block_rq_issue` tracepoints, covering every process system-wide without the per-process attach overhead ptrace has, and keeps working across kernel versions without rebuilding (no kernel headers needed at runtime).
- **AI agents recognized by process identity.** 10 mainstream AI coding-agent CLIs are matched against a built-in table; recognized events carry an `agent` field, show a badge in the live feed, and get a dedicated timeline lane so you can see what one agent actually touched.
- **TLS/DNS/SQL plaintext capture.** eCapture-style uprobes on `SSL_read`/`SSL_write`/`getaddrinfo`/`exec_simple_query` recover plaintext in the target process's own memory, before/after encryption, with no MITM proxy or certificate handling needed.
- **Two renderings per event.** Every event gets a real syscall-form professional rendering and a separately generated plain-language explanation with an everyday analogy, produced by an offline template/lookup engine (no LLM calls, fully deterministic, works with no network at all).
- **One binary, cross-platform.** Pure Go, no CGO anywhere (SQLite included). Root + Linux/amd64 gets the full eBPF pipeline; any other OS, architecture, or no root falls back to a gopsutil-based poller.

## Features

- **AI agent recognition** — every event's process is checked against a table of mainstream AI coding-agent CLIs (Claude Code, Codex, Cursor, Gemini CLI, Grok CLI, Aider, OpenCode, ZCode, OpenClacky, Antigravity CLI — the same roster CC-Monitor tracks on its dev branch); matched events carry an `agent`/`agent_display` field, get a 🤖 badge in the live feed, and the timeline page has a dedicated "by AI agent" lane so you can see exactly what an agent touched, separate from everything else running on the box.
- **Live event feed** — every exec, file open/write/unlink/rename, network connect/listen, mmap/mprotect/ptrace/cross-process memory access, kernel module load, and block-device IO, streamed over WebSocket.
- **Risk rules** — a small JSON ruleset (editable, user-overridable) flags things like reading `/etc/shadow`, W+X memory pages, `ptrace` attach, cross-process memory writes, kernel module loads, SSH activity, and more, at info/low/medium/high severity.
- **Plain-language explanations** — every event gets both a technical one-liner (`mprotect(addr=0x7f.., prot=RWX)`) and a plain-English explanation with an everyday analogy, generated offline by a template/lookup engine (no LLM calls, fully deterministic).
- **eCapture-style application probes** — best-effort uprobes on `getaddrinfo` (DNS), OpenSSL `SSL_read`/`SSL_write` (TLS plaintext), and Postgres `exec_simple_query` (SQL text), attached automatically when the target library/binary is present; each one degrades gracefully (and says so in `/api/info`) if it isn't.
- **Full web dashboard** — overview with live CPU/memory/disk/network charts, live event table, swimlane timeline (by category, by process, or by AI agent), network connections, disk IO (including a sector-level scatter plot), per-process memory maps (`/proc/<pid>/maps` visualised and explained region-by-region), a process tree, an alerts view, and a glossary of terms — styled like Grafana (dark panel grid, legends, time-range picker) but self-contained, no Grafana install required.
- **Cross-platform by design** — pure Go, no CGO (SQLite via `modernc.org/sqlite`). Builds for darwin/windows/linux on amd64/arm64; only the eBPF collector is linux/amd64-specific, everything else runs everywhere.

## Supported AI agent CLIs

Recognition is by process `comm`/`exe` identity (see `internal/agents`), mirroring the roster CC-Monitor tracks on its dev branch. Filter any of these in the API/UI via `?agent=<id>`, or watch the timeline's "by AI agent" lane.

| Agent | id | Detected via (comm / exe) |
|---|---|---|
| Claude Code | `claude-code` | `claude` |
| Codex CLI | `codex` | `codex`, `codex-x86_64-*`, `codex-aarch64-*` |
| Cursor | `cursor` | `cursor-agent` |
| Gemini CLI | `gemini-cli` | `gemini` |
| Grok CLI | `grok-cli` | `grok` |
| Aider | `aider` | `aider` |
| OpenCode | `opencode` | `opencode` |
| ZCode | `zcode` | `zcode`, `zcode-cli` |
| OpenClacky | `openclacky` | `openclacky`, `clacky` |
| Antigravity CLI | `antigravity-cli` | `agy`, `antigravity-cli` |

New agents are a one-line addition to `internal/agents/agents.go` — PRs welcome for anything missing.

## Quick start

```sh
# Build (clang/bpftool not required — the eBPF object ships pre-built)
make build

# Run as root for the eBPF collector; without root it auto-falls-back to polling
sudo ./argusbpf

# Open the dashboard
open http://127.0.0.1:1024
```

Useful flags:

| Flag | Default | Meaning |
|---|---|---|
| `--listen` | `127.0.0.1:1024` | HTTP listen address |
| `--token` | *(none)* | Require `X-Token` header / `?token=` query param |
| `--db` | `~/.argusbpf/events.db` | SQLite database path |
| `--rules` | `~/.argusbpf/rules.json` | Override the built-in risk ruleset |

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
- Distro-packaged Postgres binaries are typically LTO-built and stripped, so `exec_simple_query` isn't attachable by name; `install.sh`'s uprobe logic falls back to resolving its address from a matching `-dbgsym` debug package when one is installed, and that capability is simply unavailable without it.

# Changelog

*[中文说明](CHANGELOG.zh-CN.md)*

All notable changes to ArgusBPF are documented here. Dates are when the
work landed on `master`, not necessarily a tagged release.

## [Unreleased]

### Added
- **MCP server** (`--mcp`) — runs as a [Model Context Protocol](https://modelcontextprotocol.io/) stdio server instead of the dashboard, exposing four read-only tools to AI clients: `hardware_info` (CPU/memory/disk/host, gathered live via gopsutil), `busybox_applets` (detects and lists BusyBox's provided commands — useful on embedded/automotive images), `recent_events` and `event_stats` (the dashboard's own captured activity, filterable by category/risk/agent/time window). Reads the dashboard's SQLite database directly (new `store.OpenReadOnly`) rather than proxying over HTTP, so it needs no root, no open port, and no `--token`. Built on the official `github.com/modelcontextprotocol/go-sdk`.

## [1.1] - 2026-10-02

### Added
- **AI Activity page**, filterable by one of the 10 recognized AI agent
  CLIs or all of them combined: total/risk breakdowns, matched security
  rules (reusing the real rule engine, not a separate detector), exec
  commands classified into buckets (git/ssh/docker/download/archive/
  netdiag/install-by-package-manager/reverse-engineering), network
  footprint (distinct hosts contacted), and top processes. Every number
  on the page is a drilldown into the underlying events.
- **Optional PTY web terminal** (`--enable-terminal`, off by default) —
  open a real terminal in the dashboard and run anything in it, with
  multiple concurrent sessions and a grid view to watch several at once.
  Ported from CC-Monitor's "Terminal Sessions" tab. A genuinely different
  trust model than the rest of this read-only tool, so it's gated behind
  an explicit flag, warns at startup if turned on without `--token`, and
  its routes don't exist on the server at all unless enabled.
- `exec()` events now capture the actual command line (up to 6 argv
  entries), not just the resolved executable path — Timeline/Live Events
  previously showed `/usr/bin/git` with no way to see it was `git commit
  -m fix`.
- Startup now warns when `--listen` isn't loopback-only, and (separately)
  when `--enable-terminal` is on without `--token`; prints the full
  access URL (with token, if set) alongside the usual banner.
- `install.sh` gained `--enable-terminal`/`--disable-terminal`/`--token
  VALUE`/`--no-token`; re-running it without them now preserves an
  existing systemd unit's current setting instead of silently resetting
  it on every redeploy.
- Screenshots of all pages (English and Chinese separately) embedded at
  the top of both READMEs.

### Changed
- eBPF event delivery switched from `BPF_MAP_TYPE_RINGBUF` to
  `BPF_MAP_TYPE_PERF_EVENT_ARRAY`: at least one BTF-capable field kernel
  (a patched 5.4 automotive build) never backported ringbuf and rejected
  its creation outright; perf_event_array has existed since ~4.3 and
  runs everywhere CO-RE does.
- Builds now set `CGO_ENABLED=0` explicitly. Without it, the host's
  default silently produced a binary dynamically linked against its own
  glibc instead of the static, portable one this project is meant to be
  — it kept working on the machine that built it and only broke when
  copied elsewhere (a different distro, an embedded/automotive image).

### Fixed
- The Overview page's time-range picker changed the label but not the
  data: `/api/stats` and `/api/system/history` ignored `since`/`until`
  entirely, and the frontend only ever fetched history once at page
  load. Both now actually scope to the selected window.
- `--token` gated the static file server along with the API, so loading
  the page with `?token=...` worked but every `<link>`/`<script>` the
  browser then requested on its own (no way to attach the token to
  those) came back 401 — a fully unstyled, non-functional page. Only
  API/WebSocket routes are gated now.
- A wrong or missing token produced no visible symptom at all (every API
  call silently swallows its own errors) beyond odd blank panels now
  shows a clear banner instead.
- Terminal's "New window" button opened an actual new browser tab —
  checked against CC-Monitor's own implementation, it's supposed to open
  another terminal pane in the same page. Also made the grid view's
  refresh incremental instead of tearing down and reconnecting every
  pane's PTY on every poll.
- Several hardcoded Chinese strings that survived the original bilingual
  pass, found by actually reviewing an English screenshot: chart axis/
  legend labels, relative-time suffixes, WebSocket status text,
  well-known port/service names, Timeline's category-lane labels, and
  assorted process-detail/empty-state strings.

## [1.0] - 2026-10-01

### Added
- Initial release (as Unix-Monitor, renamed to ArgusBPF before tagging):
  single Go binary, eBPF-based (Linux/amd64, CO-RE) process/file/network/
  memory/disk/kernel activity monitor, falling back to gopsutil polling
  everywhere else.
- Every event gets a technical one-liner and a separately generated
  plain-language explanation with an everyday analogy (offline template/
  lookup engine, no LLM calls), in both Chinese and English.
- 21-rule risk engine (editable, user-overridable), info/low/medium/high
  severity.
- eCapture-style uprobes for TLS (`SSL_read`/`SSL_write`), DNS
  (`getaddrinfo`), and Postgres SQL (`exec_simple_query`) plaintext
  capture, with a dbgsym-based address-resolution fallback for stripped/
  LTO-built distro binaries.
- AI coding-agent CLI recognition (Claude Code, Codex, Cursor, Gemini
  CLI, Grok CLI, Aider, OpenCode, ZCode, OpenClacky, Antigravity CLI) by
  process identity.
- Full web dashboard: overview, live event feed, swimlane timeline (by
  category/process/agent), network, disk (with a sector-level scatter
  plot), per-process memory maps, process tree, alerts, and a glossary —
  10 themes, dark/light, fully bilingual UI.
- Cross-platform build (darwin/windows/linux, amd64/arm64); only the
  eBPF collector itself is linux/amd64-specific.

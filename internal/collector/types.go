// Package collector produces a stream of raw event.Event values from the
// operating system: an eBPF-based collector on Linux/amd64 when running as
// root with kernel BTF available, falling back everywhere else (other
// arches, other OSes, no root) to a cross-platform poller built on
// gopsutil. Neither implementation sets Risk/Rule/Pro/Plain/ID/TS — that is
// the job of the rules and explain packages, applied by the caller after
// receiving an event from the channel returned by Start.
package collector

import (
	"context"

	"argusbpf/internal/event"
)

// Info describes which backend is active and what it could/couldn't hook.
type Info struct {
	Backend  string   // "ebpf" or "poll"
	Caps     []string // e.g. "process", "file", "net", "memory", "disk", "tls", "dns", "sql"
	Warnings []string // human-readable, surfaced in /api/info and the UI's warn-bar
}

// Collector streams OS events until ctx is cancelled.
type Collector interface {
	Start(ctx context.Context) (<-chan *event.Event, error)
	Info() Info
	Close() error
}

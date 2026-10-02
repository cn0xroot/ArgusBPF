package collector

import (
	"context"
	"strconv"
	"strings"
	"time"

	"unix-monitor/internal/event"
	"unix-monitor/internal/sysinfo"
)

// Poller is the cross-platform fallback collector: it diffs periodic
// gopsutil snapshots of processes and sockets instead of hooking the
// kernel, so process start times and syscall-level detail (memory, exact
// file ops) are not available — only what the diff can infer.
type Poller struct {
	interval time.Duration
	warnings []string
}

// NewPoller creates a Poller. extraWarnings is surfaced in /api/info, e.g.
// explaining why eBPF wasn't used on this platform.
func NewPoller(extraWarnings ...string) *Poller {
	return &Poller{interval: time.Second, warnings: extraWarnings}
}

func (p *Poller) Info() Info {
	return Info{Backend: "poll", Caps: []string{"process", "net"}, Warnings: p.warnings}
}
func (p *Poller) Close() error { return nil }

func (p *Poller) Start(ctx context.Context) (<-chan *event.Event, error) {
	out := make(chan *event.Event, 256)
	go p.run(ctx, out)
	return out, nil
}

func (p *Poller) run(ctx context.Context, out chan<- *event.Event) {
	defer close(out)
	knownPids := map[int32]sysinfo.ProcInfo{}
	knownConns := map[string]bool{}
	first := true

	tick := time.NewTicker(p.interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}

		procs := sysinfo.Processes()
		seen := map[int32]bool{}
		for _, pr := range procs {
			seen[pr.PID] = true
			if _, ok := knownPids[pr.PID]; !ok && !first {
				send(out, ctx, &event.Event{
					TS: time.Now().UnixMilli(), Cat: event.CatProcess, Type: "exec",
					PID: pr.PID, PPID: pr.PPID, UID: pr.UID, User: pr.User, Comm: pr.Comm, Exe: pr.Exe,
					Fields: map[string]any{"path": pr.Exe, "args": pr.Cmdline},
				})
			}
		}
		for pid, pr := range knownPids {
			if !seen[pid] {
				send(out, ctx, &event.Event{
					TS: time.Now().UnixMilli(), Cat: event.CatProcess, Type: "exit",
					PID: pid, PPID: pr.PPID, UID: pr.UID, User: pr.User, Comm: pr.Comm, Exe: pr.Exe,
					Fields: map[string]any{"code": ""},
				})
			}
		}
		knownPids = map[int32]sysinfo.ProcInfo{}
		for _, pr := range procs {
			knownPids[pr.PID] = pr
		}

		conns := sysinfo.Connections()
		seenConns := map[string]bool{}
		for _, c := range conns {
			key := c.Proto + "|" + c.Local + "|" + c.Remote + "|" + strconv.Itoa(int(c.PID))
			seenConns[key] = true
			if knownConns[key] || first {
				continue
			}
			if c.State == "LISTEN" {
				send(out, ctx, &event.Event{
					TS: time.Now().UnixMilli(), Cat: event.CatNet, Type: "listen",
					PID: c.PID, Comm: c.Comm,
					Fields: map[string]any{"port": lastPort(c.Local), "service": c.Service},
				})
			} else if c.Remote != "" && (c.State == "ESTABLISHED" || c.State == "SYN_SENT") {
				host, port := splitAddr(c.Remote)
				send(out, ctx, &event.Event{
					TS: time.Now().UnixMilli(), Cat: event.CatNet, Type: "connect",
					PID: c.PID, Comm: c.Comm,
					Fields: map[string]any{"host": host, "port": port, "service": c.Service},
				})
			}
		}
		knownConns = seenConns
		first = false
	}
}

func send(out chan<- *event.Event, ctx context.Context, ev *event.Event) {
	select {
	case out <- ev:
	case <-ctx.Done():
	}
}

func splitAddr(addr string) (string, string) {
	i := strings.LastIndex(addr, ":")
	if i < 0 {
		return addr, ""
	}
	return addr[:i], addr[i+1:]
}
func lastPort(addr string) string {
	_, p := splitAddr(addr)
	return p
}

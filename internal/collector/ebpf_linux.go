//go:build linux && amd64

package collector

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"

	"argusbpf/internal/event"
)

// EBPFCollector hooks the kernel via the CO-RE program in bpf/monitor.c.
// Requires root (or CAP_BPF+CAP_PERFMON) and kernel BTF; NewEBPF returns an
// error otherwise so the caller can fall back to the Poller.
type EBPFCollector struct {
	objs     MonitorObjects
	links    []io.Closer
	reader   *ringbuf.Reader
	warnings []string
	caps     []string
}

// NewEBPF loads and attaches the eBPF program. It does not start reading
// events until Start is called.
func NewEBPF() (*EBPFCollector, error) {
	if err := rlimit.RemoveMemlock(); err != nil {
		return nil, fmt.Errorf("remove memlock rlimit: %w", err)
	}
	var objs MonitorObjects
	if err := LoadMonitorObjects(&objs, nil); err != nil {
		return nil, fmt.Errorf("load eBPF objects: %w", err)
	}
	c := &EBPFCollector{objs: objs, caps: []string{"process", "file", "net", "memory", "disk", "kernel"}}

	// Tell the kernel side our own pid so it can skip our events.
	key := uint32(0)
	cfg := MonitorCfg{SelfPid: uint32(os.Getpid())}
	if err := objs.MonitorCfg.Update(&key, &cfg, 0); err != nil {
		c.Close()
		return nil, fmt.Errorf("set self pid: %w", err)
	}

	attach := func(group, name string, prog *ebpf.Program) {
		l, err := link.Tracepoint(group, name, prog, nil)
		if err != nil {
			c.warnings = append(c.warnings, fmt.Sprintf("tracepoint %s/%s 挂载失败: %v", group, name, err))
			return
		}
		c.links = append(c.links, l)
	}
	attach("raw_syscalls", "sys_enter", objs.HandleSysEnter)
	attach("module", "module_load", objs.HandleModuleLoad)
	attach("block", "block_rq_issue", objs.HandleBlockRqIssue)

	uplinks, upcaps, upwarn := attachUprobes(&objs.MonitorPrograms)
	c.links = append(c.links, uplinks...)
	c.caps = append(c.caps, upcaps...)
	c.warnings = append(c.warnings, upwarn...)

	r, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("open ringbuf reader: %w", err)
	}
	c.reader = r
	return c, nil
}

func (c *EBPFCollector) Info() Info {
	return Info{Backend: "ebpf", Caps: c.caps, Warnings: c.warnings}
}

func (c *EBPFCollector) Close() error {
	if c.reader != nil {
		c.reader.Close()
	}
	for _, l := range c.links {
		l.Close()
	}
	c.objs.Close()
	return nil
}

func (c *EBPFCollector) Start(ctx context.Context) (<-chan *event.Event, error) {
	out := make(chan *event.Event, 1024)
	go func() {
		defer close(out)
		go func() { <-ctx.Done(); c.reader.Close() }()
		for {
			rec, err := c.reader.Read()
			if err != nil {
				return // reader closed (ctx done) or fatal
			}
			raw, ok := decodeRaw(rec.RawSample)
			if !ok {
				continue
			}
			ev, ok := toEvent(raw)
			if !ok {
				continue
			}
			select {
			case out <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

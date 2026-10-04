// Package mcpserver exposes ArgusBPF's already-collected system state to AI
// clients over the Model Context Protocol (MCP): hardware info, BusyBox
// applet listing, and the recorded event stream, as a handful of read-only
// tools. Run with `argusbpf --mcp` (see main.go), which an MCP client spawns
// as a stdio subprocess per the protocol's usual command-based transport.
//
// This reads the same SQLite database the main daemon writes to (see
// store.OpenReadOnly) instead of spawning its own collector or proxying
// over HTTP: the daemon has already done the eBPF capture, rule matching,
// and bilingual explanation work for every stored event, so reading its
// database directly gets identical data to what the web UI shows, with no
// need for this process to run as root, bind a port, or know a --token.
package mcpserver

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"

	"argusbpf/internal/store"
)

// Run opens the store read-only and serves MCP tools over stdio until the
// client disconnects or ctx is cancelled.
func Run(ctx context.Context, dbPath string) error {
	st, err := store.OpenReadOnly(dbPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	server := mcp.NewServer(&mcp.Implementation{Name: "argusbpf", Version: "1.1.0"}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name: "hardware_info",
		Description: "Get this machine's hardware and OS info: CPU model/core count/current usage, " +
			"memory (total/used/available/swap), disk partitions with capacity and usage, and host/kernel/uptime.",
	}, hardwareInfoHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name: "busybox_applets",
		Description: "List the applets (built-in commands) BusyBox provides on this system, if it's present. " +
			"Common on embedded/automotive Linux images where most of /bin is one BusyBox binary with symlinks " +
			"pointing at it, so this is useful for knowing what commands actually exist before trying to run them.",
	}, busyboxAppletsHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name: "recent_events",
		Description: "Get recent system activity events captured by ArgusBPF's eBPF monitor (process exec, " +
			"file, network, memory, disk, kernel, security), each with a plain-language description. Use this " +
			"for \"what's been happening on this machine\" / live-activity questions.",
	}, recentEventsHandler(st))

	mcp.AddTool(server, &mcp.Tool{
		Name: "event_stats",
		Description: "Get an aggregate summary of recent events: totals by category, by risk level, by event " +
			"type, and the most active processes. Use this for a quick overview before pulling individual " +
			"events with recent_events.",
	}, eventStatsHandler(st))

	return server.Run(ctx, &mcp.StdioTransport{})
}

// ---- hardware_info ----------------------------------------------------------

type emptyArgs struct{}

type hostInfoOut struct {
	Hostname      string `json:"hostname"`
	OS            string `json:"os"`
	Platform      string `json:"platform"`
	KernelVersion string `json:"kernel_version"`
	Arch          string `json:"arch"`
	UptimeSeconds uint64 `json:"uptime_seconds"`
}
type cpuInfoOut struct {
	ModelName     string  `json:"model_name"`
	PhysicalCores int     `json:"physical_cores"`
	LogicalCores  int     `json:"logical_cores"`
	UsagePercent  float64 `json:"usage_percent"`
}
type memoryInfoOut struct {
	TotalBytes     uint64  `json:"total_bytes"`
	UsedBytes      uint64  `json:"used_bytes"`
	AvailableBytes uint64  `json:"available_bytes"`
	UsedPercent    float64 `json:"used_percent"`
	SwapTotalBytes uint64  `json:"swap_total_bytes"`
	SwapUsedBytes  uint64  `json:"swap_used_bytes"`
}
type diskInfoOut struct {
	Device      string  `json:"device"`
	Mountpoint  string  `json:"mountpoint"`
	Fstype      string  `json:"fstype"`
	TotalBytes  uint64  `json:"total_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	FreeBytes   uint64  `json:"free_bytes"`
	UsedPercent float64 `json:"used_percent"`
}
type hardwareInfoOut struct {
	Host   hostInfoOut   `json:"host"`
	CPU    cpuInfoOut    `json:"cpu"`
	Memory memoryInfoOut `json:"memory"`
	Disks  []diskInfoOut `json:"disks"`
}

func hardwareInfoHandler(ctx context.Context, _ *mcp.CallToolRequest, _ emptyArgs) (*mcp.CallToolResult, hardwareInfoOut, error) {
	var out hardwareInfoOut

	if hi, err := host.InfoWithContext(ctx); err == nil {
		out.Host = hostInfoOut{
			Hostname: hi.Hostname, OS: hi.OS, Platform: hi.Platform + " " + hi.PlatformVersion,
			KernelVersion: hi.KernelVersion, Arch: hi.KernelArch, UptimeSeconds: hi.Uptime,
		}
	}

	if infos, err := cpu.InfoWithContext(ctx); err == nil && len(infos) > 0 {
		out.CPU.ModelName = infos[0].ModelName
	}
	if n, err := cpu.CountsWithContext(ctx, false); err == nil {
		out.CPU.PhysicalCores = n
	}
	if n, err := cpu.CountsWithContext(ctx, true); err == nil {
		out.CPU.LogicalCores = n
	}
	// A short real interval gives an actual instantaneous reading; percpu=false
	// requests must then return exactly one aggregate figure.
	if pct, err := cpu.PercentWithContext(ctx, 200*time.Millisecond, false); err == nil && len(pct) > 0 {
		out.CPU.UsagePercent = pct[0]
	}

	if vm, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		out.Memory.TotalBytes, out.Memory.UsedBytes = vm.Total, vm.Used
		out.Memory.AvailableBytes, out.Memory.UsedPercent = vm.Available, vm.UsedPercent
	}
	if sm, err := mem.SwapMemoryWithContext(ctx); err == nil {
		out.Memory.SwapTotalBytes, out.Memory.SwapUsedBytes = sm.Total, sm.Used
	}

	if parts, err := disk.PartitionsWithContext(ctx, false); err == nil {
		for _, p := range parts {
			if p.Fstype == "squashfs" {
				// Always a read-only loop-mounted package image (snap's
				// own packaging format on Ubuntu/Debian systems), never
				// real storage - reports as permanently 100% used and
				// there's one per installed snap, which would otherwise
				// drown out every actual disk in the list.
				continue
			}
			u, err := disk.UsageWithContext(ctx, p.Mountpoint)
			if err != nil {
				continue
			}
			out.Disks = append(out.Disks, diskInfoOut{
				Device: p.Device, Mountpoint: p.Mountpoint, Fstype: p.Fstype,
				TotalBytes: u.Total, UsedBytes: u.Used, FreeBytes: u.Free, UsedPercent: u.UsedPercent,
			})
		}
	}

	return nil, out, nil
}

// ---- busybox_applets ---------------------------------------------------------

type busyboxArgs struct {
	Path string `json:"path,omitempty" jsonschema:"explicit path to the busybox binary; auto-detected from common locations and PATH if omitted"`
}
type busyboxOut struct {
	Found   bool     `json:"found"`
	Path    string   `json:"path,omitempty"`
	Applets []string `json:"applets,omitempty"`
	Message string   `json:"message,omitempty"`
}

var busyboxCandidates = []string{"/bin/busybox", "/usr/bin/busybox", "/sbin/busybox", "/usr/sbin/busybox"}

func busyboxAppletsHandler(ctx context.Context, _ *mcp.CallToolRequest, args busyboxArgs) (*mcp.CallToolResult, busyboxOut, error) {
	path := args.Path
	if path == "" {
		for _, c := range busyboxCandidates {
			if st, err := os.Stat(c); err == nil && !st.IsDir() {
				path = c
				break
			}
		}
	}
	if path == "" {
		if p, err := exec.LookPath("busybox"); err == nil {
			path = p
		}
	}
	if path == "" {
		return nil, busyboxOut{Found: false, Message: "no busybox binary found in common locations or PATH"}, nil
	}

	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, path, "--list").Output()
	if err != nil {
		return nil, busyboxOut{Found: true, Path: path, Message: fmt.Sprintf("found at %s but `--list` failed: %v", path, err)}, nil
	}
	var applets []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			applets = append(applets, line)
		}
	}
	return nil, busyboxOut{Found: true, Path: path, Applets: applets}, nil
}

// ---- recent_events -----------------------------------------------------------

type recentEventsArgs struct {
	Limit        int    `json:"limit,omitempty" jsonschema:"max events to return, default 50, capped at 500"`
	Category     string `json:"category,omitempty" jsonschema:"filter: process, file, net, memory, disk, kernel, or security"`
	Risk         string `json:"risk,omitempty" jsonschema:"filter: info, low, medium, or high (an exact level, not a minimum threshold)"`
	Agent        string `json:"agent,omitempty" jsonschema:"filter: only events from this recognized AI agent CLI id, e.g. claude-code, codex, cursor"`
	SinceMinutes int    `json:"since_minutes,omitempty" jsonschema:"only events from the last N minutes; omit for no time limit"`
}
type eventOut struct {
	ID          int64  `json:"id"`
	Time        string `json:"time"` // RFC3339 UTC
	Category    string `json:"category"`
	Type        string `json:"type"`
	Risk        string `json:"risk"`
	Process     string `json:"process"`
	Agent       string `json:"agent,omitempty"`
	RuleTitle   string `json:"rule_title,omitempty"`
	Description string `json:"description"`
	Technical   string `json:"technical"`
	Count       int    `json:"count,omitempty"`
}
type recentEventsOut struct {
	Events []eventOut `json:"events"`
	Total  int        `json:"total"`
}

// pickEn prefers the English field when present, falling back to the
// Chinese one all events carry - matching the UI's own evText() logic, so
// an AI client not told otherwise gets English by default.
func pickEn(en, zh string) string {
	if en != "" {
		return en
	}
	return zh
}

func recentEventsHandler(st *store.Store) func(context.Context, *mcp.CallToolRequest, recentEventsArgs) (*mcp.CallToolResult, recentEventsOut, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args recentEventsArgs) (*mcp.CallToolResult, recentEventsOut, error) {
		limit := args.Limit
		if limit <= 0 || limit > 500 {
			limit = 50
		}
		f := store.Filter{Cat: args.Category, Risk: args.Risk, Agent: args.Agent, Limit: limit}
		if args.SinceMinutes > 0 {
			f.Since = time.Now().Add(-time.Duration(args.SinceMinutes) * time.Minute).UnixMilli()
		}
		events, err := st.Query(f)
		if err != nil {
			return nil, recentEventsOut{}, err
		}
		out := recentEventsOut{Events: make([]eventOut, 0, len(events))}
		for _, e := range events {
			out.Events = append(out.Events, eventOut{
				ID: e.ID, Time: time.UnixMilli(e.TS).UTC().Format(time.RFC3339),
				Category: string(e.Cat), Type: e.Type, Risk: string(e.Risk),
				Process:     fmt.Sprintf("%s (pid %d)", e.Comm, e.PID),
				Agent:       e.AgentDisplay,
				RuleTitle:   pickEn(e.RuleTitleEn, e.RuleTitle),
				Description: pickEn(e.PlainEn, e.Plain),
				Technical:   e.Pro,
				Count:       e.Count,
			})
		}
		out.Total = len(out.Events)
		return nil, out, nil
	}
}

// ---- event_stats ---------------------------------------------------------------

type eventStatsArgs struct {
	SinceMinutes int `json:"since_minutes,omitempty" jsonschema:"look back this many minutes, default 60"`
}
type topProcessOut struct {
	Comm  string `json:"comm"`
	PID   int32  `json:"pid"`
	Count int64  `json:"count"`
}
type eventStatsOut struct {
	Total        int64            `json:"total"`
	ByCategory   map[string]int64 `json:"by_category"`
	ByRisk       map[string]int64 `json:"by_risk"`
	ByType       map[string]int64 `json:"by_type"`
	TopProcesses []topProcessOut  `json:"top_processes"`
}

func eventStatsHandler(st *store.Store) func(context.Context, *mcp.CallToolRequest, eventStatsArgs) (*mcp.CallToolResult, eventStatsOut, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args eventStatsArgs) (*mcp.CallToolResult, eventStatsOut, error) {
		m := args.SinceMinutes
		if m <= 0 {
			m = 60
		}
		since := time.Now().Add(-time.Duration(m) * time.Minute).UnixMilli()
		stats, err := st.Stats(0, since, 0, "", false)
		if err != nil {
			return nil, eventStatsOut{}, err
		}
		out := eventStatsOut{Total: stats.Total, ByCategory: stats.ByCat, ByRisk: stats.ByRisk, ByType: stats.ByType}
		for _, p := range stats.TopProcs {
			out.TopProcesses = append(out.TopProcesses, topProcessOut{Comm: p.Comm, PID: p.PID, Count: p.N})
		}
		return nil, out, nil
	}
}

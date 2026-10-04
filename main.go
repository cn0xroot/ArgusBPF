// Command argusbpf runs the collector, HTTP API and Web UI described
// in DESIGN.md: it watches process/file/network/memory/disk/kernel
// activity (via eBPF on Linux/amd64 as root, falling back to a polling
// collector everywhere else) and serves a dashboard with both a
// professional and a plain-language view of what it sees.
//
// With --mcp, it instead runs as an MCP stdio server (see
// internal/mcpserver) exposing that same captured state to AI clients.
package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"argusbpf/internal/collector"
	"argusbpf/internal/mcpserver"
	"argusbpf/internal/rules"
	"argusbpf/internal/server"
	"argusbpf/internal/store"
)

// isLoopbackListen reports whether addr (a --listen value) only accepts
// connections from this machine. Used purely to decide whether to warn, so
// it errs toward "not loopback" (returns false) for anything it can't
// positively confirm - an unresolvable hostname, an empty host (Go's
// net/http treats ":1024" as "every interface"), or anything else.
func isLoopbackListen(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

//go:embed web
var webAssets embed.FS

func main() {
	listen := flag.String("listen", "127.0.0.1:1024", "HTTP listen address")
	token := flag.String("token", "", "optional access token (header X-Token or ?token=)")
	dbPath := flag.String("db", "", "SQLite database path (default ~/.argusbpf/events.db)")
	rulesPath := flag.String("rules", "", "user rules.json override path (default ~/.argusbpf/rules.json)")
	enableTerminal := flag.Bool("enable-terminal", false, "enable the PTY-backed web terminal (can spawn real processes from the UI; off by default, strongly recommend pairing with --token)")
	runMCP := flag.Bool("mcp", false, "run as an MCP (Model Context Protocol) stdio server exposing hardware info, BusyBox applets, and recent events as tools for an AI client, instead of the web dashboard")
	flag.Parse()

	if *runMCP {
		// A separate, lightweight mode: an MCP client (an AI assistant)
		// spawns this as a stdio subprocess per-request, so it must not
		// also try to bind the HTTP port or start the eBPF collector -
		// it only reads the database the long-running dashboard instance
		// (if any) already writes to. See internal/mcpserver.
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		if err := mcpserver.Run(ctx, *dbPath); err != nil {
			log.Fatalf("mcp server: %v", err)
		}
		return
	}

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	re := rules.LoadEngine(*rulesPath)

	col := collector.New()
	defer col.Close()

	srv := server.New(st, re, col.Info(), *token, *enableTerminal)
	if !isLoopbackListen(*listen) {
		log.Printf("警告：监听地址 %s 并非仅本机可访问，网络上能连到这台机器的任何人都能看到/操作这个面板（如果开了 --enable-terminal，也包括打开终端）/ warning: listen address %s is not loopback-only - anyone on the network who can reach this machine can see/use this dashboard (including the terminal feature, if --enable-terminal is on)", *listen, *listen)
	}
	if *enableTerminal && *token == "" {
		log.Println("警告：已启用 --enable-terminal 但未设置 --token，任何能访问此端口的人都可以打开终端 / warning: --enable-terminal is on with no --token set - anyone who can reach this port can open a shell")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	events, err := col.Start(ctx)
	if err != nil {
		log.Fatalf("start collector: %v", err)
	}
	go func() {
		for ev := range events {
			srv.Pipeline().Ingest(ev)
		}
	}()

	go srv.RunSampler(ctx)
	go srv.RunPruner(ctx)

	webRoot, err := fs.Sub(webAssets, "web")
	if err != nil {
		log.Fatalf("embed web assets: %v", err)
	}

	httpSrv := &http.Server{Addr: *listen, Handler: srv.Routes(webRoot)}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
	}()

	info := col.Info()
	fmt.Printf("ArgusBPF 已启动：http://%s （采集后端：%s）\n", *listen, info.Backend)
	if *token != "" {
		// The token was already typed in cleartext as a CLI flag (visible
		// in `ps`, the systemd unit file, shell history, ...), so echoing
		// it back here isn't a new exposure - it just saves having to go
		// dig it back out of one of those places every time.
		fmt.Printf("访问地址（含 token）：http://%s/?token=%s\n", *listen, *token)
		fmt.Printf("Access URL (with token): http://%s/?token=%s\n", *listen, *token)
	}
	if len(info.Warnings) > 0 {
		fmt.Println("提示：")
		for _, w := range info.Warnings {
			fmt.Println("  - " + w)
		}
	}
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("http server: %v", err)
	}
}

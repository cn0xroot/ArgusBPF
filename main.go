// Command argusbpf runs the collector, HTTP API and Web UI described
// in DESIGN.md: it watches process/file/network/memory/disk/kernel
// activity (via eBPF on Linux/amd64 as root, falling back to a polling
// collector everywhere else) and serves a dashboard with both a
// professional and a plain-language view of what it sees.
package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"argusbpf/internal/collector"
	"argusbpf/internal/rules"
	"argusbpf/internal/server"
	"argusbpf/internal/store"
)

//go:embed web
var webAssets embed.FS

func main() {
	listen := flag.String("listen", "127.0.0.1:1024", "HTTP listen address")
	token := flag.String("token", "", "optional access token (header X-Token or ?token=)")
	dbPath := flag.String("db", "", "SQLite database path (default ~/.argusbpf/events.db)")
	rulesPath := flag.String("rules", "", "user rules.json override path (default ~/.argusbpf/rules.json)")
	flag.Parse()

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	re := rules.LoadEngine(*rulesPath)

	col := collector.New()
	defer col.Close()

	srv := server.New(st, re, col.Info(), *token)

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

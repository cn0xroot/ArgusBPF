// Package server exposes the HTTP/JSON API and WebSocket push consumed by
// web/, and hosts the Pipeline that turns collector events into stored,
// explained rows.
package server

import (
	"context"
	"io/fs"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/host"

	"argusbpf/internal/collector"
	"argusbpf/internal/rules"
	"argusbpf/internal/store"
	"argusbpf/internal/sysinfo"
)

type Server struct {
	st    *store.Store
	rules *rules.Engine
	hub   *Hub
	pipe  *Pipeline
	info  collector.Info
	token string

	startedMs int64
	selfPID   int

	mu      sync.RWMutex
	history []sysinfo.Snapshot
	latest  sysinfo.Snapshot
	sampler *sysinfo.Sampler
}

func New(st *store.Store, re *rules.Engine, info collector.Info, token string) *Server {
	hub := NewHub()
	return &Server{
		st: st, rules: re, hub: hub, pipe: NewPipeline(st, re, hub),
		info: info, token: token, startedMs: time.Now().UnixMilli(),
		selfPID: os.Getpid(), sampler: sysinfo.NewSampler(),
	}
}

// Pipeline exposes the pipeline so main can pump collector events into it.
func (s *Server) Pipeline() *Pipeline { return s.pipe }

// RunSampler periodically snapshots system stats, keeps a 10-minute
// history ring and pushes each sample over the WebSocket.
func (s *Server) RunSampler(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		snap := s.sampler.Sample()
		s.mu.Lock()
		s.latest = snap
		s.history = append(s.history, snap)
		if len(s.history) > 600 {
			s.history = s.history[len(s.history)-600:]
		}
		s.mu.Unlock()
		s.hub.BroadcastSys(snap)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// RunPruner periodically trims old/excess rows so the DB doesn't grow
// without bound.
func (s *Server) RunPruner(ctx context.Context) {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		_ = s.st.Prune(7*24*time.Hour, 500000)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Routes returns the HTTP handler: the JSON API, /ws, and the embedded
// web UI as a catch-all static file server.
func (s *Server) Routes(webFS fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/info", s.handleInfo)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("GET /api/stats", s.handleStats)
	mux.HandleFunc("GET /api/timeline", s.handleTimeline)
	mux.HandleFunc("GET /api/system", s.handleSystem)
	mux.HandleFunc("GET /api/system/history", s.handleSystemHistory)
	mux.HandleFunc("GET /api/processes", s.handleProcesses)
	mux.HandleFunc("GET /api/process/{pid}", s.handleProcessDetail)
	mux.HandleFunc("GET /api/connections", s.handleConnections)
	mux.HandleFunc("GET /api/disk", s.handleDisk)
	mux.HandleFunc("GET /api/rules", s.handleRules)
	mux.HandleFunc("GET /api/glossary", s.handleGlossary)
	mux.HandleFunc("/ws", s.hub.ServeWS)
	mux.Handle("/", noStore(http.FileServerFS(webFS)))
	return s.withToken(mux)
}

// noStore disables browser caching for the embedded web UI. It's rebuilt
// into the binary on every change (go:embed), so a stale cached copy is
// pure confusion with no upside — unlike the API routes, these are tiny,
// local-only static files, so there's no real cost to always refetching.
func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withToken(next http.Handler) http.Handler {
	if s.token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := r.Header.Get("X-Token")
		if tok == "" {
			tok = r.URL.Query().Get("token")
		}
		if tok != s.token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func hostInfo() (name, osName, kernel, arch string) {
	name, _ = os.Hostname()
	osName = runtime.GOOS
	arch = runtime.GOARCH
	if ver, err := host.KernelVersion(); err == nil {
		kernel = ver
	}
	return
}

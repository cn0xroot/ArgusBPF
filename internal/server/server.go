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
	"argusbpf/internal/terminal"
)

type Server struct {
	st    *store.Store
	rules *rules.Engine
	hub   *Hub
	pipe  *Pipeline
	info  collector.Info
	token string

	// term is nil unless --enable-terminal was passed: the PTY-backed web
	// terminal is a genuinely different trust model than the rest of this
	// project's read-only observation (it can spawn and drive real
	// processes), so it defaults off and its routes only exist on the mux
	// at all when this is non-nil (see Routes).
	term *terminal.Manager

	startedMs int64
	selfPID   int

	mu      sync.RWMutex
	history []sysinfo.Snapshot
	latest  sysinfo.Snapshot
	sampler *sysinfo.Sampler
}

func New(st *store.Store, re *rules.Engine, info collector.Info, token string, enableTerminal bool) *Server {
	hub := NewHub()
	var term *terminal.Manager
	if enableTerminal {
		term = terminal.NewManager()
	}
	return &Server{
		st: st, rules: re, hub: hub, pipe: NewPipeline(st, re, hub),
		info: info, token: token, term: term, startedMs: time.Now().UnixMilli(),
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
//
// --token only gates the API/WS routes, not the static files: the HTML/CSS/
// JS shell carries no data of its own (it fetches everything at runtime
// through the already-gated API), and index.html has no way to attach
// ?token= to the <link>/<script> tags the browser then requests on its
// own - gating them too meant the page itself would load (its own URL
// has ?token=) but every stylesheet and script it references would 401,
// leaving a content-less, unstyled shell with nothing functional in it.
func (s *Server) Routes(webFS fs.FS) http.Handler {
	mux := http.NewServeMux()
	h := func(f http.HandlerFunc) http.Handler { return s.withToken(f) }
	mux.Handle("GET /api/info", h(s.handleInfo))
	mux.Handle("GET /api/events", h(s.handleEvents))
	mux.Handle("GET /api/stats", h(s.handleStats))
	mux.Handle("GET /api/timeline", h(s.handleTimeline))
	mux.Handle("GET /api/system", h(s.handleSystem))
	mux.Handle("GET /api/system/history", h(s.handleSystemHistory))
	mux.Handle("GET /api/processes", h(s.handleProcesses))
	mux.Handle("GET /api/process/{pid}", h(s.handleProcessDetail))
	mux.Handle("GET /api/connections", h(s.handleConnections))
	mux.Handle("GET /api/disk", h(s.handleDisk))
	mux.Handle("GET /api/rules", h(s.handleRules))
	mux.Handle("GET /api/glossary", h(s.handleGlossary))
	mux.Handle("/ws", h(s.hub.ServeWS))
	if s.term != nil {
		mux.Handle("GET /api/terminal/sessions", h(s.handleTerminalList))
		mux.Handle("POST /api/terminal/sessions", h(s.handleTerminalCreate))
		mux.Handle("DELETE /api/terminal/sessions/{id}", h(s.handleTerminalClose))
		mux.Handle("/ws/terminal/{id}", h(s.handleTerminalWS))
	}
	mux.Handle("/", noStore(http.FileServerFS(webFS)))
	return mux
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

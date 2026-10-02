package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"argusbpf/internal/agents"
	"argusbpf/internal/glossary"
	"argusbpf/internal/store"
	"argusbpf/internal/sysinfo"
)

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func qInt(r *http.Request, name string, def int) int {
	if v := r.URL.Query().Get(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
func qInt64(r *http.Request, name string, def int64) int64 {
	if v := r.URL.Query().Get(name); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	name, osName, kernel, arch := hostInfo()
	writeJSON(w, map[string]any{
		"version": "0.1.0", "host": name, "os": osName, "kernel": kernel, "arch": arch,
		"backend": s.info.Backend, "caps": s.info.Caps, "self_pid": s.selfPID,
		"started": s.startedMs, "warnings": s.info.Warnings,
	})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var pid int32
	if v, err := strconv.Atoi(q.Get("pid")); err == nil {
		pid = int32(v)
	}
	f := store.Filter{
		Cat: q.Get("cat"), Type: q.Get("type"), Risk: q.Get("risk"), Q: q.Get("q"), Agent: q.Get("agent"),
		PID: pid, Since: qInt64(r, "since", 0), Until: qInt64(r, "until", 0),
		Before: qInt64(r, "before", 0), Limit: qInt(r, "limit", 200),
	}
	events, err := s.st.Query(f)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, map[string]any{"events": events})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	st, err := s.st.Stats(s.startedMs)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, st)
}

func (s *Server) handleTimeline(w http.ResponseWriter, r *http.Request) {
	rng := qInt64(r, "range", 3600)
	lane := r.URL.Query().Get("lane")
	if lane == "" {
		lane = "cat"
	}
	tl, err := s.st.Timeline(rng, lane)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	// "cat" lane names are raw category keys (file/process/net/...); the
	// server doesn't know the client's language, so leave Label unset and
	// let the frontend translate them (see catLabel() in web/app.js).
	if lane == "agent" {
		for i := range tl.Lanes {
			tl.Lanes[i].Label = "🤖 " + agents.DisplayName(tl.Lanes[i].Name)
		}
	}
	writeJSON(w, tl)
}

func (s *Server) handleSystem(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	writeJSON(w, s.latest)
}

func (s *Server) handleSystemHistory(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	writeJSON(w, map[string]any{"samples": s.history})
}

func (s *Server) handleProcesses(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"procs": sysinfo.Processes()})
}

func (s *Server) handleProcessDetail(w http.ResponseWriter, r *http.Request) {
	pid, err := strconv.Atoi(r.PathValue("pid"))
	if err != nil {
		http.Error(w, "bad pid", 400)
		return
	}
	d, ok := sysinfo.Detail(int32(pid))
	if !ok {
		http.Error(w, "not found", 404)
		return
	}
	writeJSON(w, d)
}

func (s *Server) handleConnections(w http.ResponseWriter, r *http.Request) {
	conns := sysinfo.Connections()
	for i := range conns {
		conns[i].Plain, conns[i].PlainEn = connPlain(conns[i])
	}
	writeJSON(w, map[string]any{"conns": conns})
}

func connPlain(c sysinfo.Conn) (zh, en string) {
	if c.State == "LISTEN" {
		return c.Comm + " 正在等待外部连接进来", c.Comm + " is waiting for incoming connections"
	}
	if c.Remote != "" {
		return c.Comm + " 正在和 " + c.Remote + " 通信", c.Comm + " is communicating with " + c.Remote
	}
	return "", ""
}

func (s *Server) handleDisk(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	latest := s.latest
	s.mu.RUnlock()

	events, _ := s.st.Query(store.Filter{Cat: "disk", Limit: 1500})
	var blocks []map[string]any
	for _, ev := range events {
		sector, _ := strconv.ParseInt(ev.Str("sector"), 10, 64)
		length, _ := strconv.ParseInt(ev.Str("bytes"), 10, 64)
		blocks = append(blocks, map[string]any{
			"ts": ev.TS, "dev": ev.Str("dev"), "sector": sector,
			"len": length, "op": ev.Type, "pid": ev.PID, "comm": ev.Comm,
		})
	}
	writeJSON(w, map[string]any{
		"devices": latest.Disks, "top_files": []any{}, "top_procs": sysinfo.TopProcsByIO(10),
		"blocks": blocks,
	})
}

func (s *Server) handleRules(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"rules": s.rules.Rules()})
}

func (s *Server) handleGlossary(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"terms": glossary.Terms})
}

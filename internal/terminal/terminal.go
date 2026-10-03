// Package terminal implements an optional, explicitly opt-in PTY-backed web
// terminal (gated by the --enable-terminal flag in main.go) that lets the
// web UI spawn and interact with a real shell/CLI session end to end - a
// genuinely different trust model than the rest of this project's
// read-only eBPF observation, which is why it defaults to off and the
// routes it needs are only ever registered on the mux when enabled (see
// internal/server.Routes).
package terminal

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
)

const (
	// maxSessions caps concurrent PTY-backed processes so the web UI can't
	// be used to fork-bomb the host.
	maxSessions = 12
	// scrollbackCap bounds how much output a session keeps for a viewer
	// that (re)attaches after missing some of it (switching tabs, opening
	// grid view, a page reload).
	scrollbackCap = 256 * 1024
	readBufSize   = 4096
)

// Info is a Session's externally-visible state (what /api/terminal/sessions
// returns).
type Info struct {
	ID      string `json:"id"`
	Cmd     string `json:"cmd"`
	Started int64  `json:"started"`
	Alive   bool   `json:"alive"`
}

// Session is one PTY-backed process and its output history/subscribers.
type Session struct {
	id      string
	cmd     string
	started int64

	mu         sync.Mutex
	f          *os.File // pty master
	proc       *exec.Cmd
	alive      bool
	scrollback []byte
	subs       map[chan []byte]struct{}
}

func (s *Session) ID() string { return s.id }

func (s *Session) Info() Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Info{ID: s.id, Cmd: s.cmd, Started: s.started, Alive: s.alive}
}

// Attach registers a new live-output subscriber. Callers should write
// `scrollback` to the viewer first, then forward whatever arrives on ch
// until unsub is called (e.g. on WS disconnect).
func (s *Session) Attach() (scrollback []byte, ch chan []byte, unsub func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch = make(chan []byte, 64)
	s.subs[ch] = struct{}{}
	scrollback = append([]byte(nil), s.scrollback...)
	unsub = func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}
	return scrollback, ch, unsub
}

// Write sends input (typed keystrokes, pasted text) to the PTY.
func (s *Session) Write(p []byte) error {
	s.mu.Lock()
	f, alive := s.f, s.alive
	s.mu.Unlock()
	if !alive {
		return fmt.Errorf("session %s is not alive", s.id)
	}
	_, err := f.Write(p)
	return err
}

func (s *Session) Resize(cols, rows uint16) error {
	s.mu.Lock()
	f, alive := s.f, s.alive
	s.mu.Unlock()
	if !alive {
		return nil // a resize racing the process exiting isn't an error worth surfacing
	}
	return pty.Setsize(f, &pty.Winsize{Cols: cols, Rows: rows})
}

// Close kills the underlying process and releases the PTY. Safe to call
// more than once.
func (s *Session) Close() {
	s.mu.Lock()
	if !s.alive {
		s.mu.Unlock()
		return
	}
	s.alive = false
	proc, f := s.proc, s.f
	s.mu.Unlock()
	if proc != nil && proc.Process != nil {
		_ = proc.Process.Kill()
	}
	_ = f.Close()
}

func (s *Session) broadcast(b []byte) {
	s.mu.Lock()
	if len(b) > 0 {
		s.scrollback = append(s.scrollback, b...)
		if len(s.scrollback) > scrollbackCap {
			s.scrollback = s.scrollback[len(s.scrollback)-scrollbackCap:]
		}
	}
	subs := make([]chan []byte, 0, len(s.subs))
	for ch := range s.subs {
		subs = append(subs, ch)
	}
	s.mu.Unlock()
	// Each subscriber gets its own copy: they read concurrently and at
	// their own pace, so sharing one backing array would race.
	for _, ch := range subs {
		cp := append([]byte(nil), b...)
		select {
		case ch <- cp:
		default: // a slow/stuck viewer shouldn't block the PTY reader or other viewers
		}
	}
}

func (s *Session) readLoop() {
	buf := make([]byte, readBufSize)
	for {
		n, err := s.f.Read(buf)
		if n > 0 {
			s.broadcast(buf[:n])
		}
		if err != nil {
			break
		}
	}
	s.mu.Lock()
	s.alive = false
	s.mu.Unlock()
	s.broadcast(nil) // nudge subscribers so they notice the session ended
}

// Manager owns every live session.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
	next     int64
}

func NewManager() *Manager { return &Manager{sessions: map[string]*Session{}} }

func (m *Manager) List() []Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Info, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s.Info())
	}
	return out
}

func (m *Manager) Get(id string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	return s, ok
}

// Create spawns `sh -c cmdStr` (the default shell if cmdStr is empty)
// behind a new PTY and starts streaming its output.
func (m *Manager) Create(cmdStr string) (*Session, error) {
	m.mu.Lock()
	if len(m.sessions) >= maxSessions {
		m.mu.Unlock()
		return nil, fmt.Errorf("too many open terminal sessions (max %d) - close one first", maxSessions)
	}
	m.next++
	id := fmt.Sprintf("t%d", m.next)
	m.mu.Unlock()

	display := cmdStr
	if cmdStr == "" {
		cmdStr = defaultShell()
		display = cmdStr
	}
	// Run through `sh -c` rather than splitting cmdStr ourselves, so
	// ordinary shell syntax (quoting, env vars, pipes) works exactly like
	// it would in a real terminal.
	c := exec.Command("sh", "-c", cmdStr)
	c.Env = append(os.Environ(), "TERM=xterm-256color")
	f, err := pty.StartWithSize(c, &pty.Winsize{Cols: 80, Rows: 24})
	if err != nil {
		return nil, err
	}
	s := &Session{
		id: id, cmd: display, started: time.Now().UnixMilli(),
		f: f, proc: c, alive: true,
		subs: map[chan []byte]struct{}{},
	}
	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()
	go s.readLoop()
	return s, nil
}

// Close kills and forgets a session.
func (m *Manager) Close(id string) {
	m.mu.Lock()
	s, ok := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()
	if ok {
		s.Close()
	}
}

func defaultShell() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	return "/bin/bash"
}

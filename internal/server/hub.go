package server

import (
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"argusbpf/internal/event"
	"argusbpf/internal/sysinfo"
)

// wsMsg is the envelope sent over /ws: {"t":"events","d":[...]} or
// {"t":"sys","d":{...}}.
type wsMsg struct {
	T string `json:"t"`
	D any    `json:"d"`
}

type client struct {
	conn *websocket.Conn
	send chan wsMsg
}

// Hub fans out events and system snapshots to every connected /ws client,
// batching events emitted within a short window into one message so a
// burst of syscalls doesn't mean a burst of WebSocket frames.
type Hub struct {
	mu      sync.Mutex
	clients map[*client]bool

	batchMu sync.Mutex
	batch   []*event.Event
}

func NewHub() *Hub {
	h := &Hub{clients: map[*client]bool{}}
	go h.flushLoop()
	return h
}

var upgrader = websocket.Upgrader{
	ReadBufferSize: 1024, WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool { return true }, // local tool; token middleware already gates access
}

func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &client{conn: conn, send: make(chan wsMsg, 64)}
	h.mu.Lock()
	h.clients[c] = true
	h.mu.Unlock()

	go func() {
		defer func() {
			h.mu.Lock()
			delete(h.clients, c)
			h.mu.Unlock()
			conn.Close()
		}()
		for msg := range c.send {
			if err := conn.WriteJSON(msg); err != nil {
				return
			}
		}
	}()

	// Drain (and discard) anything the client sends; we only push.
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			close(c.send)
			return
		}
	}
}

// BroadcastEvent queues ev for the next batched "events" push (≤200ms).
func (h *Hub) BroadcastEvent(ev *event.Event) {
	h.batchMu.Lock()
	h.batch = append(h.batch, ev)
	h.batchMu.Unlock()
}

func (h *Hub) flushLoop() {
	t := time.NewTicker(200 * time.Millisecond)
	defer t.Stop()
	for range t.C {
		h.batchMu.Lock()
		if len(h.batch) == 0 {
			h.batchMu.Unlock()
			continue
		}
		batch := h.batch
		h.batch = nil
		h.batchMu.Unlock()
		h.broadcast(wsMsg{T: "events", D: batch})
	}
}

// BroadcastSys pushes a system snapshot immediately (already ~1/s cadence).
func (h *Hub) BroadcastSys(s sysinfo.Snapshot) {
	h.broadcast(wsMsg{T: "sys", D: s})
}

func (h *Hub) broadcast(msg wsMsg) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c.send <- msg:
		default: // slow client, drop
		}
	}
}

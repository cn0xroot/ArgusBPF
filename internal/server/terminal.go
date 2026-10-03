package server

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/websocket"
)

// handleTerminalList/Create/Close are only ever registered on the mux when
// --enable-terminal was passed (s.term != nil); see Routes.

func (s *Server) handleTerminalList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"sessions": s.term.List()})
}

func (s *Server) handleTerminalCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Cmd string `json:"cmd"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	sess, err := s.term.Create(body.Cmd)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, sess.Info())
}

func (s *Server) handleTerminalClose(w http.ResponseWriter, r *http.Request) {
	s.term.Close(r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

var termUpgrader = websocket.Upgrader{
	ReadBufferSize: 4096, WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool { return true }, // local tool; token middleware already gates access
}

// termCtl is the JSON shape of a text-frame control message in either
// direction: {"type":"resize","cols":80,"rows":24} from the client,
// {"type":"exit"} from the server when the underlying process ends.
// Everything else (keystrokes, pasted text, PTY output) travels as binary
// frames with no envelope, straight into/out of xterm.js.
type termCtl struct {
	Type string `json:"type"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

func (s *Server) handleTerminalWS(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.term.Get(r.PathValue("id"))
	if !ok {
		http.Error(w, "no such session", http.StatusNotFound)
		return
	}
	conn, err := termUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	scrollback, ch, unsub := sess.Attach()
	// stop, not ch, is what tells the writer goroutine below to exit when
	// the client disconnects: closing ch itself here would race
	// Session.broadcast, which can already be mid-send to it (it snapshots
	// subscribers under its own lock, then sends outside that lock) - a
	// dedicated channel that only this goroutine ever closes has no such
	// race.
	stop := make(chan struct{})
	defer func() { unsub(); close(stop) }()
	if len(scrollback) > 0 {
		if conn.WriteMessage(websocket.BinaryMessage, scrollback) != nil {
			return
		}
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case b := <-ch:
				if len(b) == 0 {
					// readLoop's EOF nudge: the process ended. Tell the
					// viewer, then force the blocked ReadMessage below to
					// return so the handler can clean up and exit instead
					// of waiting forever for client input that will now
					// never arrive alongside anything meaningful.
					_ = conn.WriteJSON(termCtl{Type: "exit"})
					_ = conn.Close()
					return
				}
				if conn.WriteMessage(websocket.BinaryMessage, b) != nil {
					return
				}
			case <-stop:
				return
			}
		}
	}()

	for {
		mt, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		switch mt {
		case websocket.BinaryMessage:
			_ = sess.Write(data)
		case websocket.TextMessage:
			var ctl termCtl
			if json.Unmarshal(data, &ctl) == nil && ctl.Type == "resize" && ctl.Cols > 0 && ctl.Rows > 0 {
				_ = sess.Resize(ctl.Cols, ctl.Rows)
			}
		}
	}
	<-done
}

// Package store persists events to SQLite (pure Go driver, no CGO) and
// answers the queries the HTTP API needs: recent events with filters,
// aggregate stats, and time-bucketed timelines.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"unix-monitor/internal/event"
)

type Store struct {
	db *sql.DB
}

// Open creates/opens the SQLite database at path (default
// ~/.unix-monitor/events.db) and ensures the schema exists.
func Open(path string) (*Store, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		dir := filepath.Join(home, ".unix-monitor")
		_ = os.MkdirAll(dir, 0o755)
		path = filepath.Join(dir, "events.db")
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // modernc.org/sqlite is not safe for concurrent writers
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS events (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	ts INTEGER NOT NULL,
	cat TEXT NOT NULL,
	type TEXT NOT NULL,
	pid INTEGER, ppid INTEGER, tid INTEGER, uid INTEGER,
	user TEXT, comm TEXT, exe TEXT,
	risk TEXT NOT NULL DEFAULT 'info',
	rule TEXT, rule_title TEXT,
	title TEXT, pro TEXT, plain TEXT, analogy TEXT,
	fields TEXT,
	count INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS idx_events_ts ON events(ts);
CREATE INDEX IF NOT EXISTS idx_events_cat ON events(cat, id DESC);
CREATE INDEX IF NOT EXISTS idx_events_risk ON events(risk, id DESC);
CREATE INDEX IF NOT EXISTS idx_events_pid ON events(pid, id DESC);
`)
	return err
}

// Insert stores ev and sets ev.ID/ev.TS (TS is set by the caller already;
// ID comes back from SQLite's rowid).
func (s *Store) Insert(ev *event.Event) error {
	fieldsJSON := "{}"
	if ev.Fields != nil {
		if b, err := json.Marshal(ev.Fields); err == nil {
			fieldsJSON = string(b)
		}
	}
	if ev.Count == 0 {
		ev.Count = 1
	}
	res, err := s.db.Exec(`INSERT INTO events (ts,cat,type,pid,ppid,tid,uid,user,comm,exe,risk,rule,rule_title,title,pro,plain,analogy,fields,count)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		ev.TS, ev.Cat, ev.Type, ev.PID, ev.PPID, ev.TID, ev.UID, ev.User, ev.Comm, ev.Exe,
		string(ev.Risk), ev.Rule, ev.RuleTitle, ev.Title, ev.Pro, ev.Plain, ev.Analogy, fieldsJSON, ev.Count)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	ev.ID = id
	return nil
}

// Filter selects events for Query. Risk is "", "low+", "medium+", "high".
type Filter struct {
	Cat, Type, Risk, Q string
	PID                int32
	Since, Until       int64 // unix ms, 0 = unbounded
	Before             int64 // id cursor for pagination, 0 = none
	Limit              int
}

func riskSet(min string) []string {
	switch min {
	case "low+":
		return []string{"low", "medium", "high"}
	case "medium+":
		return []string{"medium", "high"}
	case "high":
		return []string{"high"}
	default:
		return nil
	}
}

func (s *Store) Query(f Filter) ([]*event.Event, error) {
	var where []string
	var args []any
	if f.Cat != "" {
		where = append(where, "cat = ?")
		args = append(args, f.Cat)
	}
	if f.Type != "" {
		where = append(where, "type = ?")
		args = append(args, f.Type)
	}
	if rs := riskSet(f.Risk); rs != nil {
		ph := make([]string, len(rs))
		for i, r := range rs {
			ph[i] = "?"
			args = append(args, r)
		}
		where = append(where, "risk IN ("+strings.Join(ph, ",")+")")
	}
	if f.PID != 0 {
		where = append(where, "pid = ?")
		args = append(args, f.PID)
	}
	if f.Since > 0 {
		where = append(where, "ts >= ?")
		args = append(args, f.Since)
	}
	if f.Until > 0 {
		where = append(where, "ts <= ?")
		args = append(args, f.Until)
	}
	if f.Before > 0 {
		where = append(where, "id < ?")
		args = append(args, f.Before)
	}
	if f.Q != "" {
		where = append(where, "(comm LIKE ? OR exe LIKE ? OR plain LIKE ? OR pro LIKE ? OR fields LIKE ?)")
		q := "%" + f.Q + "%"
		args = append(args, q, q, q, q, q)
	}
	limit := f.Limit
	if limit <= 0 || limit > 2000 {
		limit = 200
	}
	query := "SELECT id,ts,cat,type,pid,ppid,tid,uid,user,comm,exe,risk,rule,rule_title,title,pro,plain,analogy,fields,count FROM events"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*event.Event
	for rows.Next() {
		ev, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanEvent(rows rowScanner) (*event.Event, error) {
	var ev event.Event
	var risk, fieldsJSON string
	if err := rows.Scan(&ev.ID, &ev.TS, &ev.Cat, &ev.Type, &ev.PID, &ev.PPID, &ev.TID, &ev.UID, &ev.User, &ev.Comm, &ev.Exe,
		&risk, &ev.Rule, &ev.RuleTitle, &ev.Title, &ev.Pro, &ev.Plain, &ev.Analogy, &fieldsJSON, &ev.Count); err != nil {
		return nil, err
	}
	ev.Risk = event.Risk(risk)
	if fieldsJSON != "" && fieldsJSON != "{}" {
		_ = json.Unmarshal([]byte(fieldsJSON), &ev.Fields)
	}
	return &ev, nil
}

// Stats summarizes recent activity for /api/stats and the overview page.
type Stats struct {
	Total    int64            `json:"total"`
	ByCat    map[string]int64 `json:"by_cat"`
	ByRisk   map[string]int64 `json:"by_risk"`
	ByType   map[string]int64 `json:"by_type"`
	TopProcs []TopProc        `json:"top_procs"`
	Rate     []int64          `json:"rate"` // events/sec, last 60s
	Started  int64            `json:"started"`
}
type TopProc struct {
	PID  int32  `json:"pid"`
	Comm string `json:"comm"`
	N    int64  `json:"count"`
}

func (s *Store) Stats(startedMs int64) (Stats, error) {
	st := Stats{ByCat: map[string]int64{}, ByRisk: map[string]int64{}, ByType: map[string]int64{}, Started: startedMs}

	row := s.db.QueryRow("SELECT COUNT(*) FROM events")
	if err := row.Scan(&st.Total); err != nil {
		return st, err
	}
	if err := fillCounts(s.db, "SELECT cat, COUNT(*) FROM events GROUP BY cat", st.ByCat); err != nil {
		return st, err
	}
	if err := fillCounts(s.db, "SELECT risk, COUNT(*) FROM events GROUP BY risk", st.ByRisk); err != nil {
		return st, err
	}
	if err := fillCounts(s.db, "SELECT type, COUNT(*) FROM events GROUP BY type", st.ByType); err != nil {
		return st, err
	}

	rows, err := s.db.Query(`SELECT pid, comm, COUNT(*) n FROM events WHERE ts > ? GROUP BY pid, comm ORDER BY n DESC LIMIT 10`,
		time.Now().Add(-10*time.Minute).UnixMilli())
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var tp TopProc
		if err := rows.Scan(&tp.PID, &tp.Comm, &tp.N); err == nil {
			st.TopProcs = append(st.TopProcs, tp)
		}
	}
	rows.Close()

	now := time.Now().UnixMilli()
	rrows, err := s.db.Query(`SELECT (ts-?)/1000 AS bucket, COUNT(*) FROM events WHERE ts > ? GROUP BY bucket`,
		now-60000, now-60000)
	if err != nil {
		return st, err
	}
	rate := make([]int64, 60)
	for rrows.Next() {
		var b, n int64
		if rrows.Scan(&b, &n) == nil && b >= 0 && b < 60 {
			rate[b] = n
		}
	}
	rrows.Close()
	st.Rate = rate
	return st, nil
}

func fillCounts(db *sql.DB, q string, dst map[string]int64) error {
	rows, err := db.Query(q)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var n int64
		if err := rows.Scan(&k, &n); err != nil {
			return err
		}
		dst[k] = n
	}
	return rows.Err()
}

// TimelineBucket is one time-bucketed cell in a swimlane.
type TimelineBucket struct {
	N    int    `json:"n"`
	Risk string `json:"risk"`
}
type TimelineLane struct {
	Name    string           `json:"name"`
	Label   string           `json:"label"`
	Buckets []TimelineBucket `json:"buckets"`
}
type Timeline struct {
	Start  int64          `json:"start"`
	End    int64          `json:"end"`
	Bucket int64          `json:"bucket"`
	Lanes  []TimelineLane `json:"lanes"`
}

// Timeline aggregates events into fixed-width time buckets per lane
// ("cat" or "proc"), for the swimlane chart. ~120 buckets across the range.
func (s *Store) Timeline(rangeSec int64, lane string) (Timeline, error) {
	now := time.Now().UnixMilli()
	start := now - rangeSec*1000
	nBuckets := int64(120)
	bucket := (rangeSec * 1000) / nBuckets
	if bucket < 1000 {
		bucket = 1000
	}

	laneCol := "cat"
	if lane == "proc" {
		laneCol = "comm"
	}
	// modernc sqlite has no custom scalar functions registered, so the
	// per-bucket max-risk aggregation happens in Go below instead of SQL.
	rows2, err := s.db.Query(fmt.Sprintf(`SELECT %s, (ts-?)/?, risk, COUNT(*) FROM events WHERE ts >= ? GROUP BY %s, (ts-?)/?, risk`, laneCol, laneCol),
		start, bucket, start, start, bucket)
	if err != nil {
		return Timeline{}, err
	}
	defer rows2.Close()

	type cell struct{ n int; risk string }
	data := map[string]map[int64]cell{}
	for rows2.Next() {
		var name string
		var b int64
		var risk string
		var n int
		if err := rows2.Scan(&name, &b, &risk, &n); err != nil {
			return Timeline{}, err
		}
		if data[name] == nil {
			data[name] = map[int64]cell{}
		}
		c := data[name][b]
		if event.Risk(risk).Rank() > event.Risk(c.risk).Rank() || c.n == 0 {
			c.risk = risk
		}
		c.n += n
		data[name][b] = c
	}

	tl := Timeline{Start: start, End: now, Bucket: bucket}
	for name, cells := range data {
		buckets := make([]TimelineBucket, nBuckets)
		for b, c := range cells {
			if b >= 0 && b < nBuckets {
				buckets[b] = TimelineBucket{N: c.n, Risk: c.risk}
			}
		}
		tl.Lanes = append(tl.Lanes, TimelineLane{Name: name, Label: name, Buckets: buckets})
	}
	return tl, nil
}

// Prune deletes events older than maxAge, keeping at most maxRows total.
func (s *Store) Prune(maxAge time.Duration, maxRows int64) error {
	cutoff := time.Now().Add(-maxAge).UnixMilli()
	if _, err := s.db.Exec("DELETE FROM events WHERE ts < ?", cutoff); err != nil {
		return err
	}
	var total int64
	if err := s.db.QueryRow("SELECT COUNT(*) FROM events").Scan(&total); err != nil {
		return err
	}
	if total > maxRows {
		_, err := s.db.Exec("DELETE FROM events WHERE id <= (SELECT id FROM events ORDER BY id DESC LIMIT 1 OFFSET ?)", maxRows)
		return err
	}
	return nil
}

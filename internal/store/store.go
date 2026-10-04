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

	"argusbpf/internal/event"
)

type Store struct {
	db *sql.DB
}

// Open creates/opens the SQLite database at path (default
// ~/.argusbpf/events.db) and ensures the schema exists.
// defaultPath returns ~/.argusbpf/events.db, creating the directory (but
// not the file) if needed.
func defaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	dir := filepath.Join(home, ".argusbpf")
	_ = os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, "events.db")
}

func Open(path string) (*Store, error) {
	if path == "" {
		path = defaultPath()
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

// OpenReadOnly opens an existing database without creating it or running
// migrations, for tools that only ever query a database the main daemon
// already owns and writes to (the MCP server, see internal/mcpserver) -
// SQLite's WAL mode already makes a read-only connection from a second
// process safe to use concurrently with the daemon's writer.
func OpenReadOnly(path string) (*Store, error) {
	if path == "" {
		path = defaultPath()
	}
	db, err := sql.Open("sqlite", path+"?mode=ro&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
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
	rule TEXT, rule_title TEXT, rule_title_en TEXT,
	title TEXT, pro TEXT, plain TEXT, analogy TEXT,
	title_en TEXT, plain_en TEXT, analogy_en TEXT,
	fields TEXT,
	count INTEGER NOT NULL DEFAULT 1,
	agent TEXT, agent_display TEXT
);
CREATE INDEX IF NOT EXISTS idx_events_ts ON events(ts);
CREATE INDEX IF NOT EXISTS idx_events_cat ON events(cat, id DESC);
CREATE INDEX IF NOT EXISTS idx_events_risk ON events(risk, id DESC);
CREATE INDEX IF NOT EXISTS idx_events_pid ON events(pid, id DESC);
CREATE INDEX IF NOT EXISTS idx_events_agent ON events(agent, id DESC);
`)
	if err != nil {
		return err
	}
	// Defensive ALTER for DBs created before the agent columns existed;
	// SQLite has no "ADD COLUMN IF NOT EXISTS", so just ignore the
	// "duplicate column" error on a DB that already has them.
	_, _ = s.db.Exec(`ALTER TABLE events ADD COLUMN agent TEXT`)
	_, _ = s.db.Exec(`ALTER TABLE events ADD COLUMN agent_display TEXT`)
	_, _ = s.db.Exec(`ALTER TABLE events ADD COLUMN rule_title_en TEXT`)
	_, _ = s.db.Exec(`ALTER TABLE events ADD COLUMN title_en TEXT`)
	_, _ = s.db.Exec(`ALTER TABLE events ADD COLUMN plain_en TEXT`)
	_, _ = s.db.Exec(`ALTER TABLE events ADD COLUMN analogy_en TEXT`)
	return nil
}

// nullableStr turns "" into a SQL NULL so columns like `agent` can be
// filtered with a plain `WHERE agent = ?` / grouped without empty-string
// noise, instead of every unrecognized process writing an empty row.
func nullableStr(s string) any {
	if s == "" {
		return nil
	}
	return s
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
	res, err := s.db.Exec(`INSERT INTO events (ts,cat,type,pid,ppid,tid,uid,user,comm,exe,risk,rule,rule_title,rule_title_en,title,pro,plain,analogy,title_en,plain_en,analogy_en,fields,count,agent,agent_display)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		ev.TS, ev.Cat, ev.Type, ev.PID, ev.PPID, ev.TID, ev.UID, ev.User, ev.Comm, ev.Exe,
		string(ev.Risk), ev.Rule, ev.RuleTitle, nullableStr(ev.RuleTitleEn), ev.Title, ev.Pro, ev.Plain, ev.Analogy,
		nullableStr(ev.TitleEn), nullableStr(ev.PlainEn), nullableStr(ev.AnalogyEn), fieldsJSON, ev.Count,
		nullableStr(ev.Agent), nullableStr(ev.AgentDisplay))
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

// Filter selects events for Query. Risk is "", "low+", "medium+", "high", or
// one bare level ("info"/"low"/"medium") for an exact match - the AI-activity
// page's risk-breakdown drilldown needs the exact level a bar counted, not
// "this or worse".
type Filter struct {
	Cat, Type, Risk, Q, Agent, Rule string
	PID                             int32
	ExeAnyOf                        []string // exec events whose exe basename is one of these (see CmdClassMembers)
	Since, Until                    int64    // unix ms, 0 = unbounded
	Before                          int64    // id cursor for pagination, 0 = none
	Limit                           int
}

func riskSet(min string) []string {
	switch min {
	case "low+":
		return []string{"low", "medium", "high"}
	case "medium+":
		return []string{"medium", "high"}
	case "high", "medium", "low", "info":
		return []string{min}
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
	if f.Agent != "" {
		where = append(where, "agent = ?")
		args = append(args, f.Agent)
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
	if f.Rule != "" {
		where = append(where, "rule = ?")
		args = append(args, f.Rule)
	}
	if len(f.ExeAnyOf) > 0 {
		ph := make([]string, len(f.ExeAnyOf))
		for i, name := range f.ExeAnyOf {
			ph[i] = "(exe = ? OR exe LIKE ?)"
			args = append(args, name, "%/"+name)
		}
		where = append(where, "("+strings.Join(ph, " OR ")+")")
	}
	limit := f.Limit
	if limit <= 0 || limit > 2000 {
		limit = 200
	}
	query := "SELECT id,ts,cat,type,pid,ppid,tid,uid,user,comm,exe,risk,rule,rule_title,rule_title_en,title,pro,plain,analogy,title_en,plain_en,analogy_en,fields,count,agent,agent_display FROM events"
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
	var agent, agentDisplay, ruleTitleEn, titleEn, plainEn, analogyEn sql.NullString
	if err := rows.Scan(&ev.ID, &ev.TS, &ev.Cat, &ev.Type, &ev.PID, &ev.PPID, &ev.TID, &ev.UID, &ev.User, &ev.Comm, &ev.Exe,
		&risk, &ev.Rule, &ev.RuleTitle, &ruleTitleEn, &ev.Title, &ev.Pro, &ev.Plain, &ev.Analogy,
		&titleEn, &plainEn, &analogyEn, &fieldsJSON, &ev.Count,
		&agent, &agentDisplay); err != nil {
		return nil, err
	}
	ev.Risk = event.Risk(risk)
	ev.Agent, ev.AgentDisplay = agent.String, agentDisplay.String
	ev.RuleTitleEn, ev.TitleEn, ev.PlainEn, ev.AnalogyEn = ruleTitleEn.String, titleEn.String, plainEn.String, analogyEn.String
	if fieldsJSON != "" && fieldsJSON != "{}" {
		_ = json.Unmarshal([]byte(fieldsJSON), &ev.Fields)
	}
	return &ev, nil
}

// Stats summarizes recent activity for /api/stats, the overview page, and
// (with Detail) the per-agent AI-activity page.
type Stats struct {
	Total    int64            `json:"total"`
	ByCat    map[string]int64 `json:"by_cat"`
	ByRisk   map[string]int64 `json:"by_risk"`
	ByType   map[string]int64 `json:"by_type"`
	TopProcs []TopProc        `json:"top_procs"`
	Rate     []int64          `json:"rate"` // events/sec, last 60s
	Started  int64            `json:"started"`

	// Populated only when Stats is called with detail=true (the AI-activity
	// page); left nil for the plain overview call so its frequent polling
	// doesn't pay for queries nothing reads.
	ByRule   map[string]int64 `json:"by_rule,omitempty"`   // matched rule id -> count
	CmdClass map[string]int64 `json:"cmd_class,omitempty"` // classifyExe() bucket -> count, from exec events
	TopHosts []TopHost        `json:"top_hosts,omitempty"` // distinct hosts from connect/dns events
}
type TopProc struct {
	PID  int32  `json:"pid"`
	Comm string `json:"comm"`
	N    int64  `json:"count"`
}
type TopHost struct {
	Host string `json:"host"`
	N    int64  `json:"count"`
}

// Stats summarizes activity in [sinceMs, untilMs] for one agent id (or every
// event when agent == ""); either time bound may be 0 to leave that side
// open, and (0, 0) is "all time", matching the original unscoped behavior.
// Rate is the one exception to the window - it's always the last 60 real
// seconds regardless of sinceMs/untilMs, since it's a live pulse indicator
// rather than a historical figure, and it ignores the agent filter too
// (it's the top-level "how busy is the box right now" sparkline).
func (s *Store) Stats(startedMs, sinceMs, untilMs int64, agent string, detail bool) (Stats, error) {
	st := Stats{ByCat: map[string]int64{}, ByRisk: map[string]int64{}, ByType: map[string]int64{}, Started: startedMs}

	where, args := filterClause(sinceMs, untilMs, agent)

	row := s.db.QueryRow("SELECT COUNT(*) FROM events"+where, args...)
	if err := row.Scan(&st.Total); err != nil {
		return st, err
	}
	if err := fillCounts(s.db, "SELECT cat, COUNT(*) FROM events"+where+" GROUP BY cat", args, st.ByCat); err != nil {
		return st, err
	}
	if err := fillCounts(s.db, "SELECT risk, COUNT(*) FROM events"+where+" GROUP BY risk", args, st.ByRisk); err != nil {
		return st, err
	}
	if err := fillCounts(s.db, "SELECT type, COUNT(*) FROM events"+where+" GROUP BY type", args, st.ByType); err != nil {
		return st, err
	}

	topSince := sinceMs
	if topSince == 0 {
		topSince = time.Now().Add(-10 * time.Minute).UnixMilli()
	}
	topWhere, topArgs := filterClause(topSince, untilMs, agent)
	rows, err := s.db.Query(`SELECT pid, comm, COUNT(*) n FROM events`+topWhere+` GROUP BY pid, comm ORDER BY n DESC LIMIT 10`, topArgs...)
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

	if !detail {
		return st, nil
	}

	st.ByRule = map[string]int64{}
	ruleWhere, ruleArgs := filterClause(sinceMs, untilMs, agent)
	if ruleWhere == "" {
		ruleWhere = " WHERE rule != ''"
	} else {
		ruleWhere += " AND rule != ''"
	}
	if err := fillCounts(s.db, "SELECT rule, COUNT(*) FROM events"+ruleWhere+" GROUP BY rule", ruleArgs, st.ByRule); err != nil {
		return st, err
	}

	execWhere, execArgs := filterClause(sinceMs, untilMs, agent)
	if execWhere == "" {
		execWhere = " WHERE type = 'exec'"
	} else {
		execWhere += " AND type = 'exec'"
	}
	st.CmdClass = map[string]int64{}
	erows, err := s.db.Query("SELECT exe, COUNT(*) FROM events"+execWhere+" GROUP BY exe", execArgs...)
	if err != nil {
		return st, err
	}
	for erows.Next() {
		var exe string
		var n int64
		if erows.Scan(&exe, &n) == nil {
			if bucket := classifyExe(exe); bucket != "" {
				st.CmdClass[bucket] += n
			}
		}
	}
	erows.Close()

	hostWhere, hostArgs := filterClause(sinceMs, untilMs, agent)
	hostCond := "type IN ('connect','dns')"
	if hostWhere == "" {
		hostWhere = " WHERE " + hostCond
	} else {
		hostWhere += " AND " + hostCond
	}
	hrows, err := s.db.Query(`SELECT COALESCE(json_extract(fields,'$.host'), json_extract(fields,'$.name')) AS host, COUNT(*) n
		FROM events`+hostWhere+` GROUP BY host HAVING host IS NOT NULL AND host != '' ORDER BY n DESC LIMIT 15`, hostArgs...)
	if err != nil {
		return st, err
	}
	for hrows.Next() {
		var th TopHost
		if hrows.Scan(&th.Host, &th.N) == nil {
			st.TopHosts = append(st.TopHosts, th)
		}
	}
	hrows.Close()

	return st, nil
}

// tsRangeClause builds a " WHERE ts ..." fragment for an optional
// [sinceMs, untilMs] window; either bound may be 0 to leave that side open,
// and (0, 0) yields an empty clause (no filtering at all).
func tsRangeClause(sinceMs, untilMs int64) (string, []any) {
	switch {
	case sinceMs > 0 && untilMs > 0:
		return " WHERE ts >= ? AND ts <= ?", []any{sinceMs, untilMs}
	case sinceMs > 0:
		return " WHERE ts >= ?", []any{sinceMs}
	case untilMs > 0:
		return " WHERE ts <= ?", []any{untilMs}
	default:
		return "", nil
	}
}

// filterClause extends tsRangeClause with an optional exact `agent` match,
// ANDed in when agent is non-empty.
func filterClause(sinceMs, untilMs int64, agent string) (string, []any) {
	where, args := tsRangeClause(sinceMs, untilMs)
	if agent == "" {
		return where, args
	}
	if where == "" {
		return " WHERE agent = ?", []any{agent}
	}
	return where + " AND agent = ?", append(args, agent)
}

// classifyExe buckets an exec()'d path by the shape of CLI tooling it
// represents, the same idea as CC-Monitor's command-pattern rules but
// matched against the actual resolved executable (from the exec() syscall
// itself) rather than a regex over a shell command string.
// CmdClassMembers is the single source of truth for exec-event command
// classification: classifyExe() below and the /api/events?cmdclass= drilldown
// (handleEvents) both derive from it, so the AI-activity page's "click a
// count to see the matching events" always matches what was actually
// counted.
var CmdClassMembers = map[string][]string{
	"git":               {"git", "gh"},
	"ssh":               {"ssh", "sftp", "ssh-keygen", "ssh-copy-id", "ssh-add"},
	"filesend":          {"scp", "rsync", "nc", "ncat", "netcat"},
	"docker":            {"docker", "docker-compose", "podman", "nerdctl", "containerd", "runc"},
	"download":          {"curl", "wget", "aria2c"},
	"archive":           {"tar", "zip", "unzip", "gzip", "gunzip", "bzip2", "bunzip2", "xz", "unxz", "7z", "7za", "rar", "unrar"},
	"netdiag":           {"ping", "ping6", "traceroute", "tracepath", "nmap", "dig", "nslookup", "host", "mtr"},
	"procbg":            {"nohup", "screen", "tmux", "setsid", "disown", "at", "systemd-run"},
	"install_pip":       {"pip", "pip3"},
	"install_uv":        {"uv"},
	"install_js":        {"npm", "yarn", "pnpm", "bun"},
	"install_system":    {"apt", "apt-get", "yum", "dnf", "pacman", "brew", "port", "apk", "zypper", "dpkg", "rpm"},
	"install_toolchain": {"rustup", "cargo", "nvm", "pyenv", "rbenv", "sdk", "go", "gvm"},
	"reverseeng":        {"ida", "ida64", "ghidra", "r2", "radare2", "gdb", "objdump", "readelf", "nm"},
}

var exeNameToClass = func() map[string]string {
	m := map[string]string{}
	for bucket, names := range CmdClassMembers {
		for _, n := range names {
			m[n] = bucket
		}
	}
	return m
}()

func classifyExe(path string) string {
	name := path
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	return exeNameToClass[name]
}

func fillCounts(db *sql.DB, q string, args []any, dst map[string]int64) error {
	rows, err := db.Query(q, args...)
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
	extraWhere := ""
	switch lane {
	case "proc":
		laneCol = "comm"
	case "agent":
		// Only AI coding-agent CLIs get their own lane here; everything
		// else would otherwise show up as one huge NULL lane.
		laneCol, extraWhere = "agent", " AND agent IS NOT NULL"
	}
	// modernc sqlite has no custom scalar functions registered, so the
	// per-bucket max-risk aggregation happens in Go below instead of SQL.
	rows2, err := s.db.Query(fmt.Sprintf(`SELECT %s, (ts-?)/?, risk, COUNT(*) FROM events WHERE ts >= ?%s GROUP BY %s, (ts-?)/?, risk`, laneCol, extraWhere, laneCol),
		start, bucket, start, start, bucket)
	if err != nil {
		return Timeline{}, err
	}
	defer rows2.Close()

	type cell struct {
		n    int
		risk string
	}
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

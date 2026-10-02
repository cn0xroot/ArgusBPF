package server

import (
	"fmt"
	"sync"
	"time"

	"argusbpf/internal/agents"
	"argusbpf/internal/event"
	"argusbpf/internal/explain"
	"argusbpf/internal/rules"
	"argusbpf/internal/store"
)

// Pipeline turns raw collector events into stored, explained, broadcast
// ones: Apply explain + rules, fold chatty event kinds into a single
// counted row per second, persist, and push to WS clients.
type Pipeline struct {
	st    *store.Store
	rules *rules.Engine
	hub   *Hub

	mu      sync.Mutex
	pending map[string]*event.Event
}

func NewPipeline(st *store.Store, re *rules.Engine, hub *Hub) *Pipeline {
	p := &Pipeline{st: st, rules: re, hub: hub, pending: map[string]*event.Event{}}
	go p.flushLoop()
	return p
}

// aggregateKey groups the handful of event kinds frequent enough to spam
// the UI (disk block IO, repeated mmap/mprotect/brk from the same process)
// so they fold into one row with a Count instead of one row each.
func aggregateKey(ev *event.Event) (string, bool) {
	switch string(ev.Cat) + "." + ev.Type {
	case "disk.read", "disk.write", "memory.mmap", "memory.mprotect", "memory.brk",
		"file.open", "file.write":
		return fmt.Sprintf("%s|%s|%d", ev.Cat, ev.Type, ev.PID), true
	default:
		return "", false
	}
}

// Ingest applies explain/rules to ev and either stores it immediately or
// folds it into the current 1s aggregation window.
func (p *Pipeline) Ingest(ev *event.Event) {
	if ev.Risk == "" {
		ev.Risk = event.RiskInfo
	}
	explain.Apply(ev)
	p.rules.Evaluate(ev)
	if a := agents.Match(ev.Comm, ev.Exe); a != nil {
		ev.Agent, ev.AgentDisplay = a.ID, a.Display
	}

	key, agg := aggregateKey(ev)
	if !agg {
		p.flushOne(ev)
		return
	}
	p.mu.Lock()
	if cur, ok := p.pending[key]; ok {
		cur.Count++
		cur.TS = ev.TS
		// Keep whichever sample is riskiest so far, not just the latest one
		// — folding a dozen harmless opens together must never bury the
		// one that touched /etc/shadow under a later, boring sample.
		if ev.Risk.Rank() >= cur.Risk.Rank() {
			cur.Fields = ev.Fields
			cur.Risk, cur.Rule, cur.RuleTitle, cur.RuleTitleEn = ev.Risk, ev.Rule, ev.RuleTitle, ev.RuleTitleEn
			cur.Pro, cur.Plain, cur.Analogy = ev.Pro, ev.Plain, ev.Analogy
			cur.TitleEn, cur.PlainEn, cur.AnalogyEn = ev.TitleEn, ev.PlainEn, ev.AnalogyEn
		}
		p.mu.Unlock()
		return
	}
	p.pending[key] = ev
	p.mu.Unlock()
}

func (p *Pipeline) flushLoop() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for range t.C {
		p.mu.Lock()
		pending := p.pending
		p.pending = map[string]*event.Event{}
		p.mu.Unlock()
		for _, ev := range pending {
			p.flushOne(ev)
		}
	}
}

func (p *Pipeline) flushOne(ev *event.Event) {
	if err := p.st.Insert(ev); err != nil {
		return
	}
	p.hub.BroadcastEvent(ev)
}

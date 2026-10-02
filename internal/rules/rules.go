// Package rules evaluates events against a user-configurable JSON ruleset
// to assign a risk level and a human-written rule title/description —
// the same model CC-Monitor uses for its PreToolUse hook rules, applied
// here to raw OS events instead of Claude Code tool calls.
package rules

import (
	"encoding/json"
	"os"
	"regexp"
	"sync"

	"unix-monitor/internal/event"
)

// Rule is one risk rule. Types lists the event "cat:type" or "cat" patterns
// it applies to (e.g. "file", "file:write", "net:connect"). Field names
// which event field (or "comm"/"exe"/"path"/"target") Pattern is matched
// against with a case-insensitive regexp.
type Rule struct {
	ID      string      `json:"id"`
	Risk    event.Risk  `json:"risk"`
	Types   []string    `json:"types"`
	Field   string      `json:"field"`
	Pattern string      `json:"pattern"`
	Title   string      `json:"title"`
	Desc    string      `json:"desc"`
	compile *regexp.Regexp
}

// Engine holds a compiled ruleset and evaluates events against it in order;
// the first matching rule wins (rules should be ordered highest-risk first).
type Engine struct {
	mu    sync.RWMutex
	rules []*Rule
}

// NewEngine builds an Engine from the given rules, compiling patterns.
// Rules with an invalid pattern are skipped.
func NewEngine(rs []Rule) *Engine {
	e := &Engine{}
	e.setRules(rs)
	return e
}

func (e *Engine) setRules(rs []Rule) {
	compiled := make([]*Rule, 0, len(rs))
	for i := range rs {
		r := rs[i]
		re, err := regexp.Compile("(?i)" + r.Pattern)
		if err != nil {
			continue
		}
		r.compile = re
		compiled = append(compiled, &r)
	}
	e.mu.Lock()
	e.rules = compiled
	e.mu.Unlock()
}

// Load replaces the ruleset from a JSON file (an array of Rule).
func (e *Engine) Load(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var rs []Rule
	if err := json.Unmarshal(b, &rs); err != nil {
		return err
	}
	e.setRules(rs)
	return nil
}

// Rules returns a snapshot of the current ruleset (for the /api/rules endpoint).
func (e *Engine) Rules() []Rule {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]Rule, len(e.rules))
	for i, r := range e.rules {
		out[i] = *r
		out[i].compile = nil
	}
	return out
}

func typeMatches(patterns []string, cat, typ string) bool {
	full := cat + ":" + typ
	for _, p := range patterns {
		if p == cat || p == full || p == "*" {
			return true
		}
	}
	return false
}

func fieldValue(ev *event.Event, field string) string {
	switch field {
	case "comm":
		return ev.Comm
	case "exe":
		return ev.Exe
	case "user":
		return ev.User
	case "type":
		return ev.Type
	default:
		return ev.Str(field)
	}
}

// Evaluate finds the first matching rule and applies its risk/title/desc to
// ev. If nothing matches, ev.Risk stays at whatever the caller set (normally
// event.RiskInfo) and Rule/RuleTitle are left empty.
func (e *Engine) Evaluate(ev *event.Event) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, r := range e.rules {
		if !typeMatches(r.Types, string(ev.Cat), ev.Type) {
			continue
		}
		val := fieldValue(ev, r.Field)
		if val == "" || !r.compile.MatchString(val) {
			continue
		}
		ev.Risk = r.Risk
		ev.Rule = r.ID
		ev.RuleTitle = r.Title
		return
	}
}

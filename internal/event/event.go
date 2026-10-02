// Package event defines the core event model shared across the collector,
// pipeline, store and server.
package event

// Category groups events by the kind of OS resource they touch.
type Category string

const (
	CatProcess  Category = "process"
	CatFile     Category = "file"
	CatNet      Category = "net"
	CatMemory   Category = "memory"
	CatDisk     Category = "disk"
	CatKernel   Category = "kernel"
	CatSecurity Category = "security"
)

// Risk levels, ordered.
type Risk string

const (
	RiskInfo   Risk = "info"
	RiskLow    Risk = "low"
	RiskMedium Risk = "medium"
	RiskHigh   Risk = "high"
)

// Rank returns a sortable rank for a risk level (higher is riskier).
func (r Risk) Rank() int {
	switch r {
	case RiskHigh:
		return 3
	case RiskMedium:
		return 2
	case RiskLow:
		return 1
	default:
		return 0
	}
}

// AtLeast reports whether r is at least as risky as min.
func (r Risk) AtLeast(min Risk) bool { return r.Rank() >= min.Rank() }

// Event is one observed OS activity, after enrichment, rules and explanation.
type Event struct {
	ID    int64    `json:"id"`
	TS    int64    `json:"ts"` // unix milliseconds
	Cat   Category `json:"cat"`
	Type  string   `json:"type"`

	PID  int32  `json:"pid"`
	PPID int32  `json:"ppid"`
	TID  int32  `json:"tid"`
	UID  uint32 `json:"uid"`
	User string `json:"user"`
	Comm string `json:"comm"`
	Exe  string `json:"exe"`

	Risk      Risk   `json:"risk"`
	Rule      string `json:"rule,omitempty"`
	RuleTitle string `json:"rule_title,omitempty"`

	Title   string `json:"title"`   // short professional label
	Pro     string `json:"pro"`     // professional one-line (syscall form)
	Plain   string `json:"plain"`   // plain-language explanation
	Analogy string `json:"analogy"` // everyday analogy

	Fields map[string]any `json:"fields,omitempty"`
	Count  int            `json:"count"` // number of aggregated raw events
}

// Field helpers --------------------------------------------------------------

func (e *Event) setField(k string, v any) {
	if e.Fields == nil {
		e.Fields = map[string]any{}
	}
	e.Fields[k] = v
}

// SetField stores a field value, creating the map on demand.
func (e *Event) SetField(k string, v any) { e.setField(k, v) }

// Str returns a string field or "".
func (e *Event) Str(k string) string {
	if e.Fields == nil {
		return ""
	}
	if s, ok := e.Fields[k].(string); ok {
		return s
	}
	return ""
}

package rules

import (
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
)

//go:embed default_rules.json
var defaultRulesJSON []byte

// DefaultRules returns the built-in ruleset, embedded in the binary.
func DefaultRules() []Rule {
	var rs []Rule
	_ = json.Unmarshal(defaultRulesJSON, &rs)
	return rs
}

// LoadEngine builds an Engine from the embedded defaults, then overlays the
// user's ~/.argusbpf/rules.json if present (same convention as
// CC-Monitor's ~/.cc-monitor/rules.json: entirely replaces the set, so a
// user who wants to keep the defaults should copy them first).
func LoadEngine(userPath string) *Engine {
	e := NewEngine(DefaultRules())
	if userPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return e
		}
		userPath = filepath.Join(home, ".argusbpf", "rules.json")
	}
	if _, err := os.Stat(userPath); err == nil {
		_ = e.Load(userPath)
	}
	return e
}

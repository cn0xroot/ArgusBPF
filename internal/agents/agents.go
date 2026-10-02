// Package agents recognizes mainstream AI coding-agent CLIs (Claude Code,
// Codex, Cursor, …) by process comm/exe, so the timeline and event views
// can show "what did the AI agent do" as its own dimension instead of
// lumping it in with every other "node"/"python3" process.
//
// The list mirrors CC-Monitor's agent definitions (cc_monitor/agents/*.json
// on its dev branch), matched on `process.comm` / `process.exe_basename`
// only — argv-pattern matching (used there for disambiguating things like
// bare interpreters) is intentionally left out here to keep per-event
// matching a cheap map lookup.
package agents

import "strings"

// Agent is one recognized AI coding-agent CLI.
type Agent struct {
	ID      string
	Display string
}

var all = []struct {
	Agent
	comms []string
	exes  []string
}{
	{Agent{"claude-code", "Claude Code"}, []string{"claude"}, []string{"claude"}},
	{Agent{"codex", "Codex CLI"}, []string{"codex", "codex-x86_64", "codex-aarch64"},
		[]string{"codex", "codex-x86_64-unknown-linux-musl", "codex-aarch64-unknown-linux-musl",
			"codex-aarch64-apple-darwin", "codex-x86_64-apple-darwin"}},
	{Agent{"cursor", "Cursor"}, []string{"cursor-agent"}, []string{"cursor-agent"}},
	{Agent{"gemini-cli", "Gemini CLI"}, []string{"gemini"}, []string{"gemini"}},
	{Agent{"grok-cli", "Grok CLI"}, []string{"grok"}, []string{"grok"}},
	{Agent{"aider", "Aider"}, nil, []string{"aider"}},
	{Agent{"opencode", "OpenCode"}, []string{"opencode"}, []string{"opencode"}},
	{Agent{"zcode", "ZCode"}, []string{"zcode"}, []string{"zcode", "zcode-cli"}},
	{Agent{"openclacky", "OpenClacky"}, []string{"openclacky", "clacky"}, []string{"openclacky", "clacky"}},
	{Agent{"antigravity-cli", "Antigravity CLI"}, []string{"agy"}, []string{"agy", "antigravity-cli"}},
}

var byKey = func() map[string]*Agent {
	m := map[string]*Agent{}
	for i := range all {
		a := &all[i].Agent
		for _, c := range all[i].comms {
			m[strings.ToLower(c)] = a
		}
		for _, e := range all[i].exes {
			m[strings.ToLower(e)] = a
		}
	}
	return m
}()

var byID = func() map[string]string {
	m := map[string]string{}
	for i := range all {
		m[all[i].ID] = all[i].Display
	}
	return m
}()

func basename(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// Match returns the recognized AI agent CLI for this comm/exe, or nil.
func Match(comm, exe string) *Agent {
	if a, ok := byKey[strings.ToLower(comm)]; ok {
		return a
	}
	if exe != "" {
		if a, ok := byKey[strings.ToLower(basename(exe))]; ok {
			return a
		}
	}
	return nil
}

// DisplayName returns the human label for an agent id (as stored on
// event.Event.Agent / the store's `agent` column), or id itself if unknown.
func DisplayName(id string) string {
	if d, ok := byID[id]; ok {
		return d
	}
	return id
}

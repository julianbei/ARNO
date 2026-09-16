package main

import (
	"github.com/julianbei/arno/internal/compat"

	"fmt"
	"sort"
	"strings"
	"sync"
)

var (
	toolNamesOnce sync.Once
	toolNames     map[string]bool
)

// knownToolNames is the catalog's wire names, built once from tools() so the
// alias rule can never disagree with what tools/list advertises.
func knownToolNames() map[string]bool {
	toolNamesOnce.Do(func() {
		toolNames = make(map[string]bool)
		for _, tool := range tools() {
			toolNames[tool.Name] = true
		}
	})
	return toolNames
}

// canonicalToolName maps a requested tool name to its wire name.
//
// The wire name is `arno.find`, but hosts rewrite dots out of tool names:
// Claude Code shows `mcp__arno__arno_find`, and an agent that learned the name
// from its host then sends `arno_find` over a raw session and is told the
// tool does not exist. Both spellings name the same tool, so both are
// accepted. Nothing else is guessed — a name that matches neither form is
// unknown, and the error lists near matches rather than picking one.
func canonicalToolName(name string) (string, error) {
	known := knownToolNames()
	if known[name] {
		return name, nil
	}
	if rest, ok := strings.CutPrefix(name, "arno_"); ok && known["arno."+rest] {
		return "arno." + rest, nil
	}
	// Jade's names, until the release after 0.0.12: a tester's client config
	// and any agent that learned jade.find keep working across the rename.
	if renamed := compat.ToolName(name); renamed != "" && known[renamed] {
		return renamed, nil
	}
	return "", unknownToolError(name, known)
}

func unknownToolError(name string, known map[string]bool) error {
	bare := strings.TrimPrefix(strings.TrimPrefix(name, "arno."), "arno_")
	var near []string
	for candidate := range known {
		candidateBare := strings.TrimPrefix(candidate, "arno.")
		if bare != "" && (strings.Contains(candidateBare, bare) || strings.Contains(bare, candidateBare)) {
			near = append(near, candidate)
		}
	}
	sort.Strings(near)
	if len(near) == 0 {
		return fmt.Errorf("unknown tool: %s (tool names are arno.<name>, e.g. arno.find; tools/list has the catalog)", name)
	}
	return fmt.Errorf("unknown tool: %s (did you mean %s?)", name, strings.Join(near, ", "))
}

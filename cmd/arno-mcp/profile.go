package main

import (
	"fmt"
	"strings"
)

// coreProfileTools is what `--tools core` lists: every tool an agent called in
// the benchmark pilot's ARNO runs. The full catalog is about 23 KB of schema
// that the host sends again with every turn, so a 20-turn task pays for it 20
// times; these twelve are under half of it.
//
// Unlisted tools stay callable. The profile changes what is advertised, never
// what is served, so an agent or script that names arno.rename still works.
//
// Since 0.0.8 run_command and declare_command replace outline and
// workspace_tree, which the 0.0.7 reruns called four times in 24 runs. Without
// a shell an agent could not run a reproduction or a benchmark at all, and
// run_command alone could not help: it runs only declared commands. A
// declaration is a file in the repository, so in a repository worked in for
// longer it is paid for once and reused by every later session.
var coreProfileTools = map[string]bool{
	"arno.find":            true,
	"arno.grep":            true,
	"arno.read_range":      true,
	"arno.replace_text":    true,
	"arno.insert":          true,
	"arno.apply":           true,
	"arno.check":           true,
	"arno.run_tests":       true,
	"arno.run_command":     true,
	"arno.declare_command": true,
	"arno.create_file":     true,
	"arno.delete_file":     true,
}

// toolsFlag reads --tools all|core (or --tools=core). The default is all.
// core+<tool>[,<tool>] adds named tools to the core profile — how a candidate
// for the profile is benchmarked before it earns a place in it, without a
// second hardcoded list. The 2026-09-16 round ran `inspect` that way; it
// was never called and was removed.
func toolsFlag(args []string) (string, error) {
	for i := 0; i < len(args); i++ {
		name, value, hasValue := strings.Cut(args[i], "=")
		if name != "--tools" && name != "-tools" {
			continue
		}
		if !hasValue {
			if i+1 >= len(args) {
				return "", fmt.Errorf("--tools needs a profile: all, core or core+<tool>")
			}
			value = args[i+1]
		}
		base, extras := profileParts(value)
		if base != "all" && base != "core" {
			return "", fmt.Errorf("--tools must be all, core or core+<tool>[,<tool>], got %q", value)
		}
		if base == "all" && len(extras) > 0 {
			return "", fmt.Errorf("--tools all already lists every tool, got %q", value)
		}
		known := map[string]bool{}
		for _, tool := range tools() {
			known[tool.Name] = true
		}
		for extra := range extras {
			if !known[extra] {
				return "", fmt.Errorf("--tools %s: %s is not a tool", value, extra)
			}
		}
		return value, nil
	}
	return "all", nil
}

// profileParts splits core+inspect,outline into its base and the extra tools
// as catalog names.
func profileParts(profile string) (string, map[string]bool) {
	base, rest, _ := strings.Cut(profile, "+")
	extras := map[string]bool{}
	for _, name := range strings.Split(rest, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if !strings.HasPrefix(name, "arno.") {
			name = "arno." + strings.TrimPrefix(name, "arno_")
		}
		extras[name] = true
	}
	return strings.TrimSpace(base), extras
}

// listedTools is the catalog tools/list answers with for a profile.
func listedTools(profile string) []mcpTool {
	catalog := tools()
	base, extras := profileParts(profile)
	if base != "core" {
		return catalog
	}
	listed := make([]mcpTool, 0, len(coreProfileTools)+len(extras))
	for _, tool := range catalog {
		if coreProfileTools[tool.Name] || extras[tool.Name] {
			listed = append(listed, tool)
		}
	}
	return listed
}

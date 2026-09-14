package main

import (
	"fmt"
	"strings"
)

// coreProfileTools is what `--tools core` lists: every tool an agent called in
// the benchmark pilot's Jade runs. The full catalog is about 23 KB of schema
// that the host sends again with every turn, so a 20-turn task pays for it 20
// times; these twelve are under half of it.
//
// Unlisted tools stay callable. The profile changes what is advertised, never
// what is served, so an agent or script that names jade.rename still works.
//
// Since 0.0.8 run_command and declare_command replace outline and
// workspace_tree, which the 0.0.7 reruns called four times in 24 runs. Without
// a shell an agent could not run a reproduction or a benchmark at all, and
// run_command alone could not help: it runs only declared commands. A
// declaration is a file in the repository, so in a repository worked in for
// longer it is paid for once and reused by every later session.
var coreProfileTools = map[string]bool{
	"jade.find":            true,
	"jade.grep":            true,
	"jade.read_range":      true,
	"jade.replace_text":    true,
	"jade.insert":          true,
	"jade.apply":           true,
	"jade.check":           true,
	"jade.run_tests":       true,
	"jade.run_command":     true,
	"jade.declare_command": true,
	"jade.create_file":     true,
	"jade.delete_file":     true,
}

// toolsFlag reads --tools all|core (or --tools=core). The default is all.
func toolsFlag(args []string) (string, error) {
	for i := 0; i < len(args); i++ {
		name, value, hasValue := strings.Cut(args[i], "=")
		if name != "--tools" && name != "-tools" {
			continue
		}
		if !hasValue {
			if i+1 >= len(args) {
				return "", fmt.Errorf("--tools needs a profile: all or core")
			}
			value = args[i+1]
		}
		switch value {
		case "all", "core":
			return value, nil
		}
		return "", fmt.Errorf("--tools must be all or core, got %q", value)
	}
	return "all", nil
}

// listedTools is the catalog tools/list answers with for a profile.
func listedTools(profile string) []mcpTool {
	catalog := tools()
	if profile != "core" {
		return catalog
	}
	listed := make([]mcpTool, 0, len(coreProfileTools))
	for _, tool := range catalog {
		if coreProfileTools[tool.Name] {
			listed = append(listed, tool)
		}
	}
	return listed
}

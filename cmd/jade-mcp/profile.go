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
var coreProfileTools = map[string]bool{
	"jade.find":           true,
	"jade.grep":           true,
	"jade.read_range":     true,
	"jade.outline":        true,
	"jade.replace_text":   true,
	"jade.insert":         true,
	"jade.apply":          true,
	"jade.check":          true,
	"jade.run_tests":      true,
	"jade.create_file":    true,
	"jade.delete_file":    true,
	"jade.workspace_tree": true,
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

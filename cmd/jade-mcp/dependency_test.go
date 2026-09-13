package main

import (
	"strings"
	"testing"
)

// A dependency's source is searchable and readable by name, and not editable.
func TestDependencySourceIsReadableNotWritable(t *testing.T) {
	server, root := newTestMCPServer(t)
	writeWorkspaceFile(t, root, "node_modules/left-pad/package.json", `{"version": "1.3.0"}`)
	writeWorkspaceFile(t, root, "node_modules/left-pad/index.js", "module.exports = function leftPad(str, len, ch) {\n  return str;\n};\n")

	found, err := callText(t, server, "jade.grep", map[string]interface{}{"query": "leftPad", "dependency": "left-pad"})
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if !strings.Contains(found, "dep:left-pad/index.js:1") || !strings.Contains(found, "left-pad 1.3.0") {
		t.Errorf("expected a dependency match with its version, got:\n%s", found)
	}

	read, err := callText(t, server, "jade.read_range", map[string]interface{}{"path": "dep:left-pad/index.js", "lines": "1-2"})
	if err != nil || !strings.Contains(read, "function leftPad") {
		t.Errorf("expected the dependency file, got %v:\n%s", err, read)
	}

	if _, err := callText(t, server, "jade.replace_text", map[string]interface{}{"path": "dep:left-pad/index.js", "oldText": "return str;", "newText": "return ch;"}); err == nil {
		t.Error("a dependency path must not be editable")
	}

	if _, err := callText(t, server, "jade.grep", map[string]interface{}{"query": "x", "dependency": "../outside"}); err == nil {
		t.Error("a path posing as a dependency name must be refused")
	}
}

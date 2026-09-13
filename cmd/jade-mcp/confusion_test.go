package main

import (
	"strings"
	"testing"
)

// The confusion report is built from real dispatch: targets are hashed at the
// single recording point, and the catalog comes from the served tool list.
func TestTelemetryReportsToolConfusion(t *testing.T) {
	server, root := newTestMCPServer(t)
	writeWorkspaceFile(t, root, "a.go", "package a\n\nfunc A() {}\n")

	calls := []struct {
		tool string
		args map[string]interface{}
	}{
		{"jade.read_range", map[string]interface{}{"path": "a.go"}},
		{"jade.outline", map[string]interface{}{"path": "a.go"}},
		{"jade.read_symbol", map[string]interface{}{"path": "a.go", "symbolName": "Missing"}},
		{"jade.read_symbol", map[string]interface{}{"path": "a.go", "symbolName": "A"}},
	}
	for _, call := range calls {
		if _, err := callText(t, server, call.tool, call.args); err != nil {
			t.Fatalf("%s: %v", call.tool, err)
		}
	}

	out, err := callText(t, server, "jade.telemetry", map[string]interface{}{})
	if err != nil {
		t.Fatalf("telemetry: %v", err)
	}
	for _, want := range []string{
		// outline → read_symbol on the same file is a switch too; both pairs
		// are listed, so only presence is checked.
		"switched tools on the same target: ",
		"jade.read_range→jade.outline 1",
		"retried after a failed answer: jade.read_symbol after not_found 1",
		"never called:",
		"jade.rename",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "a.go") {
		t.Errorf("the report must not reveal targets, got:\n%s", out)
	}
}

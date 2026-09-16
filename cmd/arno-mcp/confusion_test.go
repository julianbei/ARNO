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
		{"arno.read_range", map[string]interface{}{"path": "a.go"}},
		{"arno.outline", map[string]interface{}{"path": "a.go"}},
		{"arno.references", map[string]interface{}{"path": "a.go", "symbolName": "Missing"}},
		{"arno.references", map[string]interface{}{"path": "a.go", "symbolName": "A"}},
	}
	for _, call := range calls {
		// The first references call is meant to fail: the retry after it is
		// what the report counts.
		_, _ = callText(t, server, call.tool, call.args)
	}

	out, err := callText(t, server, "arno.telemetry", map[string]interface{}{})
	if err != nil {
		t.Fatalf("telemetry: %v", err)
	}
	for _, want := range []string{
		// outline → references on the same file is a switch too; both pairs
		// are listed, so only presence is checked.
		"switched tools on the same target: ",
		"arno.read_range→arno.outline 1",
		"retried after a failed answer: arno.references after not_found 1",
		"never called:",
		"arno.rename",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "a.go") {
		t.Errorf("the report must not reveal targets, got:\n%s", out)
	}
}

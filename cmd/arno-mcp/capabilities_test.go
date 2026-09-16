package main

import (
	"strings"
	"testing"

	"github.com/julianbei/arno/internal/code"
)

func TestCapabilitiesReportsLanguagesAndGaps(t *testing.T) {
	server, root := newTestMCPServer(t)
	writeWorkspaceFile(t, root, "main.go", "package main\n\nfunc main() {}\n")
	writeWorkspaceFile(t, root, "app.kt", "fun main() {}\n")

	out, err := callText(t, server, "arno.capabilities", map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"go (1 files) · structural · tree-sitter",
		"kotlin (1 files) · text fallback · text scan · may be incomplete · no server known",
		"git: ",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
func TestCapabilityBriefNamesLanguagesAndServers(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "a.go", "package a\n")
	writeWorkspaceFile(t, root, "b.go", "package a\n")
	writeWorkspaceFile(t, root, "app.kt", "fun main() {}\n")

	brief := capabilityBrief(code.NewIndex(root, nil))
	for _, want := range []string{" This workspace: go (grammar, ", "kotlin (text scan only, no server known)", "Call capabilities"} {
		if !strings.Contains(brief, want) {
			t.Fatalf("missing %q in %q", want, brief)
		}
	}
	if strings.Index(brief, "go (") > strings.Index(brief, "kotlin (") {
		t.Fatalf("languages should come by file count, got %q", brief)
	}
	if empty := capabilityBrief(code.NewIndex(t.TempDir(), nil)); empty != "" {
		t.Fatalf("an empty workspace should add nothing, got %q", empty)
	}
}

package main

import (
	"strings"
	"testing"
)

func TestCapabilitiesReportsLanguagesAndGaps(t *testing.T) {
	server, root := newTestMCPServer(t)
	writeWorkspaceFile(t, root, "main.go", "package main\n\nfunc main() {}\n")
	writeWorkspaceFile(t, root, "app.kt", "fun main() {}\n")

	out, err := callText(t, server, "jade.capabilities", map[string]interface{}{})
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

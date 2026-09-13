package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/julianbei/jade/internal/protocol"
)

// A validation chain is a declared command whose steps are joined with &&:
// tests, then a rule tool such as Semgrep. Exit status decides — a failing
// later step fails the run, and nothing after it runs. The README documents
// this as the way to add repository rules to validation without a native
// integration, so it is held here.
func TestDeclaredValidationChainStopsAtTheFirstFailingStep(t *testing.T) {
	server, root := newTestMCPServer(t)
	chain := "printf tests > step1.txt && printf 'rule: forbidden call' >&2 && exit 3 && printf never > step3.txt"
	if _, err := server.api.DeclareCommand(protocol.DeclareCommandRequest{Name: "validate", Run: chain, Description: "tests, then repository rules"}); err != nil {
		t.Fatal(err)
	}

	out, err := callText(t, server, "jade.run_command", map[string]interface{}{"name": "validate"})
	if err != nil {
		t.Fatal(err)
	}
	if head := firstLine(out); !strings.HasPrefix(head, "FAIL validate") {
		t.Fatalf("a failing rule step should fail the chain, got:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(root, "step1.txt")); err != nil {
		t.Fatalf("the step before the failure should have run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "step3.txt")); !os.IsNotExist(err) {
		t.Fatal("no step after a failure may run")
	}

	if _, err := server.api.DeclareCommand(protocol.DeclareCommandRequest{Name: "validate", Run: "true && true"}); err != nil {
		t.Fatal(err)
	}
	out, err = callText(t, server, "jade.run_command", map[string]interface{}{"name": "validate"})
	if err != nil {
		t.Fatal(err)
	}
	if head := firstLine(out); !strings.HasPrefix(head, "pass validate") {
		t.Fatalf("a chain whose every step exits 0 should pass, got:\n%s", out)
	}
}

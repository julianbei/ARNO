package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/julianbei/jade/internal/protocol"
)

func TestCheckRunsTheDeclaredCommandsOfItsKind(t *testing.T) {
	server, root := newTestMCPServer(t)

	out, err := callText(t, server, "jade.check", map[string]interface{}{"kind": "codegen"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "unavailable") || !strings.Contains(out, "declare") {
		t.Fatalf("codegen with nothing declared should say how to declare it, got:\n%s", out)
	}

	for _, request := range []protocol.DeclareCommandRequest{
		{Name: "lint-a", Run: "printf a > lint-a.txt", Kind: "lint"},
		{Name: "lint-b", Run: "printf b > lint-b.txt", Kind: "lint"},
		{Name: "gen", Run: "printf gen > gen.txt", Kind: "codegen"},
		{Name: "unrelated", Run: "printf x > unrelated.txt"},
	} {
		if _, err := server.api.DeclareCommand(request); err != nil {
			t.Fatal(err)
		}
	}

	out, err = callText(t, server, "jade.check", map[string]interface{}{"kind": "lint"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(firstLine(out), "pass") || !strings.Contains(out, "lint-a, lint-b") {
		t.Fatalf("check lint should run both declared lint commands and name them, got:\n%s", out)
	}
	for _, name := range []string{"lint-a.txt", "lint-b.txt"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatalf("%s should have been written by its lint command: %v", name, err)
		}
	}
	for _, name := range []string{"gen.txt", "unrelated.txt"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("check lint must run only lint commands, but %s exists", name)
		}
	}

	if _, err := server.api.DeclareCommand(protocol.DeclareCommandRequest{Name: "lint-a", Run: "exit 2", Kind: "lint"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "lint-b.txt")); err != nil {
		t.Fatal(err)
	}
	out, err = callText(t, server, "jade.check", map[string]interface{}{"kind": "lint"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(firstLine(out), "FAIL") {
		t.Fatalf("a failing lint command should fail the check, got:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(root, "lint-b.txt")); !os.IsNotExist(err) {
		t.Fatal("no lint command after a failing one may run")
	}
}

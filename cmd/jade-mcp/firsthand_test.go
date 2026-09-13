package main

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/julianbei/jade/internal/protocol"
)

// Fixes for what building 0.0.3 with Jade itself turned up (ROADMAP §8).

func TestReplaceTextCountsOnlyWhatChanged(t *testing.T) {
	server, root := newTestMCPServer(t)
	writeWorkspaceFile(t, root, "a.go", "package a\n\nfunc A() {}\n")

	out, err := callText(t, server, "jade.replace_text", map[string]interface{}{
		"path":    "a.go",
		"oldText": "func A() {}\n",
		"newText": "func A() {}\n\nfunc B() {}\n",
	})
	if err != nil {
		t.Fatalf("replace_text: %v", err)
	}
	if !strings.Contains(firstLine(out), "+2 -0") {
		t.Fatalf("a pure addition after a kept anchor should read +2 -0, got:\n%s", out)
	}
}

func TestSingleEditStartsNoBackgroundJob(t *testing.T) {
	server, root := newTestMCPServer(t)
	writeWorkspaceFile(t, root, "a.txt", "one\n")

	response, err := server.api.ReplaceText(protocol.ReplaceTextRequest{Path: "a.txt", OldText: "one", NewText: "two"})
	if err != nil {
		t.Fatalf("replace_text: %v", err)
	}
	if len(response.Jobs) != 0 {
		t.Fatalf("expected no background job, got %v", response.Jobs)
	}
}

func TestGrepSkipsBinaryFiles(t *testing.T) {
	server, root := newTestMCPServer(t)
	writeWorkspaceFile(t, root, "bin/tool", "\x7fELF\x00\x01needle runtime string\n")
	writeWorkspaceFile(t, root, "notes.txt", "needle in text\n")

	out, err := callText(t, server, "jade.grep", map[string]interface{}{"query": "needle"})
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if !strings.Contains(out, "notes.txt") {
		t.Fatalf("expected the text match, got:\n%s", out)
	}
	if strings.Contains(out, "bin/tool") {
		t.Fatalf("a binary file must not be searched, got:\n%s", out)
	}
}

func TestGoStructIsNotCalledAClass(t *testing.T) {
	server, root := newTestMCPServer(t)
	writeWorkspaceFile(t, root, "store.go", "package store\n\ntype Project struct{}\n")

	out, err := callText(t, server, "jade.find", map[string]interface{}{"query": "Project"})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if strings.Contains(out, "class Project") || !strings.Contains(out, "struct Project") {
		t.Fatalf("expected the Go struct labelled struct, got:\n%s", out)
	}
}

func TestReadInARepositoryWithNoCommitsIsNotFlagged(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	server, root := newTestMCPServer(t)
	if output, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	writeWorkspaceFile(t, root, "a.txt", "one\ntwo\n")

	out, err := callText(t, server, "jade.read_range", map[string]interface{}{"path": "a.txt"})
	if err != nil {
		t.Fatalf("read_range: %v", err)
	}
	if strings.Contains(out, "freshness unknown") {
		t.Fatalf("no commits yet is a normal state, got:\n%s", out)
	}
}

func TestCommandListingShowsNamesNotScripts(t *testing.T) {
	server, _ := newTestMCPServer(t)
	script := "set -e; " + strings.Repeat("echo step; ", 40)
	if _, err := server.api.DeclareCommand(protocol.DeclareCommandRequest{Name: "release-gate", Run: script, Description: "full release check"}); err != nil {
		t.Fatalf("declare: %v", err)
	}

	out, err := callText(t, server, "jade.run_command", map[string]interface{}{})
	if err != nil {
		t.Fatalf("run_command: %v", err)
	}
	if !strings.Contains(out, "release-gate") || !strings.Contains(out, "full release check") {
		t.Fatalf("expected name and description, got:\n%s", out)
	}
	if strings.Contains(out, "echo step") {
		t.Fatalf("a described command's script does not belong in the listing, got:\n%s", out)
	}
}

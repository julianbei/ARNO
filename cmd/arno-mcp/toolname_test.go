package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBothToolNameSpellingsAreAccepted(t *testing.T) {
	for _, requested := range []string{"arno.workspace_tree", "arno_workspace_tree"} {
		got, err := canonicalToolName(requested)
		if err != nil {
			t.Fatalf("%s: %v", requested, err)
		}
		if got != "arno.workspace_tree" {
			t.Fatalf("%s: canonical name %q", requested, got)
		}
	}
}

func TestEveryCatalogToolHasAnUnderscoreAlias(t *testing.T) {
	for _, tool := range tools() {
		alias := strings.Replace(tool.Name, "arno.", "arno_", 1)
		if got, err := canonicalToolName(alias); err != nil || got != tool.Name {
			t.Errorf("%s: alias %s resolved to %q, %v", tool.Name, alias, got, err)
		}
	}
}

func TestUnknownToolIsNotGuessed(t *testing.T) {
	for _, requested := range []string{"workspace_tree", "arno.nope", "mcp__arno__arno_find", "arno_"} {
		if got, err := canonicalToolName(requested); err == nil {
			t.Errorf("%s: resolved to %q; only the two exact spellings are aliases", requested, got)
		}
	}
	_, err := canonicalToolName("arno.tree")
	if err == nil || !strings.Contains(err.Error(), "arno.workspace_tree") {
		t.Fatalf("expected a near match suggestion, got %v", err)
	}
}

// The alias must go through the real dispatch path, and telemetry must record
// the canonical name so both spellings count as one tool.
func TestUnderscoreAliasDispatchesAndRecordsCanonically(t *testing.T) {
	server, root := newTestMCPServer(t)

	if _, err := server.handleToolCall(toolCall(t, "arno_changes", nil)); err != nil {
		t.Fatalf("arno_changes: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".arno", "telemetry.jsonl"))
	if err != nil {
		t.Fatalf("telemetry: %v", err)
	}
	if !strings.Contains(string(data), `"arno.changes"`) || strings.Contains(string(data), "arno_changes") {
		t.Fatalf("expected the canonical name recorded, got:\n%s", data)
	}
}

// A call Arno rejects must leave no trace in the workspace. Harnesses commit
// untracked files, so a stray telemetry file becomes part of someone's change.
func TestUnknownToolWritesNothingToTheWorkspace(t *testing.T) {
	server, root := newTestMCPServer(t)

	if _, err := server.handleToolCall(toolCall(t, "arno.does_not_exist", nil)); err == nil {
		t.Fatal("expected an error for an unknown tool")
	}
	if _, err := os.Stat(filepath.Join(root, ".arno")); !os.IsNotExist(err) {
		t.Fatalf("an unknown tool call created .arno/: %v", err)
	}
}

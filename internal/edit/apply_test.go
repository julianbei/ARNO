package edit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/julianbei/arno/internal/commands"
	"github.com/julianbei/arno/internal/protocol"
)

func writeIn(t *testing.T, dir string, name string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", name, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func contentOf(t *testing.T, dir string, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}

func TestApplyRunsEveryEditAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	svc := newTestService(t, dir)
	writeIn(t, dir, "a.go", "package main\n\nfunc A() int {\n\treturn 1\n}\n")
	writeIn(t, dir, "b.go", "package main\n\nfunc B() int {\n\treturn 2\n}\n")

	response, err := svc.Apply(protocol.ApplyRequest{
		Edits: []protocol.EditOp{
			{Op: "replace_text", Path: "a.go", OldText: "return 1", NewText: "return 11"},
			{Op: "replace_text", Path: "b.go", OldText: "return 2", NewText: "return 22"},
			{Op: "insert", Path: "a.go", Position: "end", NewText: "\nfunc Added() int { return 3 }\n"},
		},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if response.Applied != 3 {
		t.Fatalf("expected 3 edits applied, got %d", response.Applied)
	}
	if len(response.Changed) != 2 {
		t.Fatalf("expected 2 changed files, got %v", response.Changed)
	}

	if !strings.Contains(contentOf(t, dir, "a.go"), "return 11") {
		t.Fatalf("first edit did not land")
	}
	if !strings.Contains(contentOf(t, dir, "b.go"), "return 22") {
		t.Fatalf("second edit did not land")
	}
	if !strings.Contains(contentOf(t, dir, "a.go"), "func Added()") {
		t.Fatalf("insert did not land")
	}
}

func TestApplyRollsBackEverythingWhenOneEditFails(t *testing.T) {
	// The property the whole tool exists for. A scripted multi-file edit that
	// half-applied is what broke the MCP server's own build during
	// dogfooding; Apply must leave the tree exactly as it found it.
	dir := t.TempDir()
	svc := newTestService(t, dir)
	original := "package main\n\nfunc A() int {\n\treturn 1\n}\n"
	writeIn(t, dir, "a.go", original)
	writeIn(t, dir, "b.go", "package main\n\nfunc B() int {\n\treturn 2\n}\n")

	_, err := svc.Apply(protocol.ApplyRequest{
		Edits: []protocol.EditOp{
			{Op: "replace_text", Path: "a.go", OldText: "return 1", NewText: "return 11"},
			// Second edit targets a file already touched, so preflight skips
			// it and it fails at apply time — exercising rollback rather
			// than preflight rejection.
			{Op: "replace_text", Path: "a.go", OldText: "text that is not there", NewText: "x"},
		},
	})
	if err == nil {
		t.Fatalf("expected the batch to fail")
	}
	if !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("expected the error to say the batch rolled back, got %v", err)
	}

	if got := contentOf(t, dir, "a.go"); got != original {
		t.Fatalf("expected a.go restored exactly, got:\n%s", got)
	}
}

func TestApplyRejectsAmbiguousAnchorBeforeWritingAnything(t *testing.T) {
	dir := t.TempDir()
	svc := newTestService(t, dir)
	original := "package main\n\nfunc A() { x() }\n\nfunc B() { x() }\n"
	writeIn(t, dir, "a.go", original)

	_, err := svc.Apply(protocol.ApplyRequest{
		Edits: []protocol.EditOp{{Op: "replace_text", Path: "a.go", OldText: "x()", NewText: "y()"}},
	})
	if err == nil {
		t.Fatalf("expected an ambiguous anchor to be rejected")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected an ambiguity error, got %v", err)
	}
	// Preflight means nothing was written, not that it was written and undone.
	if contentOf(t, dir, "a.go") != original {
		t.Fatalf("expected the file untouched")
	}
}

func TestApplyRejectsUnknownOpBeforeWriting(t *testing.T) {
	dir := t.TempDir()
	svc := newTestService(t, dir)
	original := "package main\n"
	writeIn(t, dir, "a.go", original)

	_, err := svc.Apply(protocol.ApplyRequest{
		Edits: []protocol.EditOp{
			{Op: "replace_text", Path: "a.go", OldText: "package main", NewText: "package other"},
			{Op: "rm -rf", Path: "a.go"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown op") {
		t.Fatalf("expected an unknown op to be rejected, got %v", err)
	}
	if contentOf(t, dir, "a.go") != original {
		t.Fatalf("expected a bad op anywhere in the batch to block the whole batch")
	}
}

func TestApplyRejectsStaleRevision(t *testing.T) {
	dir := t.TempDir()
	svc := newTestService(t, dir)
	writeIn(t, dir, "a.go", "package main\n")

	_, err := svc.Apply(protocol.ApplyRequest{
		ExpectedRevision: "r-not-current",
		Edits:            []protocol.EditOp{{Op: "replace_text", Path: "a.go", OldText: "main", NewText: "other"}},
	})
	if !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("expected ErrStaleRevision, got %v", err)
	}
	// A revision the workspace cannot account for points at changes.
	if !strings.Contains(err.Error(), "expected r-not-current, workspace is at r1") || !strings.Contains(err.Error(), "changes lists what moved") {
		t.Fatalf("expected the refusal to name both revisions and point at changes, got %q", err)
	}
}

// A stale-revision refusal names the files that changed in between, so the
// agent knows what to re-read rather than only that something moved.
func TestStaleRevisionNamesChangedFiles(t *testing.T) {
	dir := t.TempDir()
	svc := newTestService(t, dir)
	writeIn(t, dir, "a.go", "package main\n")
	writeIn(t, dir, "b.go", "package main\n")
	writeIn(t, dir, "c.go", "package main\n")

	if _, err := svc.Apply(protocol.ApplyRequest{Edits: []protocol.EditOp{
		{Op: "replace_text", Path: "a.go", OldText: "main", NewText: "first"},
	}}); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if _, err := svc.Apply(protocol.ApplyRequest{Edits: []protocol.EditOp{
		{Op: "replace_text", Path: "b.go", OldText: "main", NewText: "second"},
		{Op: "replace_text", Path: "a.go", OldText: "first", NewText: "third"},
	}}); err != nil {
		t.Fatalf("second apply: %v", err)
	}

	_, err := svc.Apply(protocol.ApplyRequest{
		ExpectedRevision: "r1",
		Edits:            []protocol.EditOp{{Op: "replace_text", Path: "c.go", OldText: "main", NewText: "other"}},
	})
	var stale *StaleRevisionError
	if !errors.As(err, &stale) || !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("expected a StaleRevisionError, got %v", err)
	}
	if stale.Expected != "r1" || stale.Current != "r3" || !stale.Known {
		t.Fatalf("expected r1 vs r3 known, got %+v", stale)
	}
	if got := strings.Join(stale.Changed, ","); got != "a.go,b.go" {
		t.Fatalf("expected a.go and b.go once each, got %q", got)
	}
	want := "expected r1, workspace is at r3; changed since r1: a.go, b.go — read those again before editing, then retry with expectedRevision r3"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected %q in %q", want, err.Error())
	}
	if contentOf(t, dir, "c.go") != "package main\n" {
		t.Fatalf("expected c.go untouched after the refusal")
	}

	// Only the edits after the expected revision are listed.
	_, err = svc.Apply(protocol.ApplyRequest{
		ExpectedRevision: "r2",
		Edits:            []protocol.EditOp{{Op: "replace_text", Path: "c.go", OldText: "main", NewText: "other"}},
	})
	if !errors.As(err, &stale) || strings.Join(stale.Changed, ",") != "a.go,b.go" {
		t.Fatalf("expected the r2 to r3 bump (a.go and b.go) only, got %v", err)
	}
}

func TestApplyRejectsEmptyBatch(t *testing.T) {
	dir := t.TempDir()
	svc := newTestService(t, dir)

	if _, err := svc.Apply(protocol.ApplyRequest{}); err == nil {
		t.Fatalf("expected an empty batch to be rejected")
	}
}

func TestApplyBumpsRevisionOnce(t *testing.T) {
	// Six edits previously meant six revision bumps and six validation jobs,
	// five of them racing files that were still mid-change.
	dir := t.TempDir()
	svc := newTestService(t, dir)
	writeIn(t, dir, "a.go", "package main\n\nfunc A() int {\n\treturn 1\n}\n")

	response, err := svc.Apply(protocol.ApplyRequest{
		Edits: []protocol.EditOp{
			{Op: "replace_text", Path: "a.go", OldText: "return 1", NewText: "return 2"},
			{Op: "replace_text", Path: "a.go", OldText: "func A()", NewText: "func Renamed()"},
		},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if response.OldRevision == response.NewRevision {
		t.Fatalf("expected one revision bump, got %s → %s", response.OldRevision, response.NewRevision)
	}
	if response.Applied != 2 {
		t.Fatalf("expected 2 edits, got %d", response.Applied)
	}
}

func TestApplyDeletesAndInsertsInOneUnit(t *testing.T) {
	dir := t.TempDir()
	svc := newTestService(t, dir)
	writeIn(t, dir, "a.go", "package main\n\nfunc Keep() int {\n\treturn 1\n}\n\nfunc Drop() int {\n\treturn 2\n}\n")

	response, err := svc.Apply(protocol.ApplyRequest{
		Edits: []protocol.EditOp{
			{Op: "delete_symbol", Path: "a.go", SymbolName: "Drop"},
			{Op: "insert", Path: "a.go", Position: "end", NewText: "func Fresh() int { return 3 }\n"},
		},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if response.Applied != 2 {
		t.Fatalf("expected 2 edits, got %d", response.Applied)
	}

	content := contentOf(t, dir, "a.go")
	if strings.Contains(content, "func Drop()") {
		t.Fatalf("expected Drop deleted, got:\n%s", content)
	}
	if !strings.Contains(content, "func Fresh()") {
		t.Fatalf("expected Fresh inserted, got:\n%s", content)
	}
	if !strings.Contains(content, "func Keep()") {
		t.Fatalf("expected Keep untouched, got:\n%s", content)
	}
}

func TestApplyFormatsTouchedFiles(t *testing.T) {
	dir := t.TempDir()
	svc := newTestService(t, dir)
	writeIn(t, dir, "a.go", "package main\n\nfunc A() int {\n\treturn 1\n}\n")

	response, err := svc.Apply(protocol.ApplyRequest{
		Format: true,
		Edits: []protocol.EditOp{
			// Deliberately badly indented — gofmt must clean it up.
			{Op: "replace_text", Path: "a.go", OldText: "\treturn 1", NewText: "                    return 1"},
		},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	content := contentOf(t, dir, "a.go")
	if strings.Contains(content, "                    return") {
		t.Fatalf("expected gofmt to normalize indentation, got:\n%s", content)
	}
	if len(response.Formatted) != 1 {
		t.Fatalf("expected the formatted file reported, got %v", response.Formatted)
	}
}
func TestImpactTracesCallersAndTestsOfTouchedDeclarations(t *testing.T) {
	dir := t.TempDir()
	svc := newTestService(t, dir)
	writeIn(t, dir, "go.mod", "module example.com/impact\n\ngo 1.22\n")
	writeIn(t, dir, "a.go", "package main\n\nfunc Target() int {\n\treturn 1\n}\n\nfunc Untouched() int {\n\treturn 0\n}\n")
	writeIn(t, dir, "b.go", "package main\n\nfunc Caller() int {\n\treturn Target()\n}\n")
	writeIn(t, dir, "a_test.go", "package main\n\nimport \"testing\"\n\nfunc TestTarget(t *testing.T) {\n\tif Target() != 2 {\n\t\tt.Fatal(\"wrong\")\n\t}\n}\n")

	edits := []protocol.EditOp{{Op: "replace_text", Path: "a.go", OldText: "return 1", NewText: "return 2"}}
	if _, err := svc.Apply(protocol.ApplyRequest{Edits: edits}); err != nil {
		t.Fatal(err)
	}

	impact, files := svc.impact(edits, []string{"a.go"}, nil, 0)
	if impact.Declarations != 1 {
		t.Fatalf("only Target was touched, got %d declarations", impact.Declarations)
	}
	if impact.CallerFiles != 1 || strings.Join(impact.Tests, ",") != "a_test.go" {
		t.Fatalf("expected b.go as the caller file and a_test.go as the test, got %+v", impact)
	}
	joined := strings.Join(files, ",")
	for _, want := range []string{"a.go", "b.go", "a_test.go"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("files to validate should include %s, got %v", want, files)
		}
	}
}
func TestImpactTracesWhatADeletionBreaks(t *testing.T) {
	dir := t.TempDir()
	svc := newTestService(t, dir)
	writeIn(t, dir, "go.mod", "module example.com/deleted\n\ngo 1.22\n")
	writeIn(t, dir, "a.go", "package main\n\nfunc Target() int {\n\treturn 1\n}\n")
	writeIn(t, dir, "b.go", "package main\n\nfunc Caller() int {\n\treturn Target()\n}\n")
	writeIn(t, dir, "a_test.go", "package main\n\nimport \"testing\"\n\nfunc TestTarget(t *testing.T) {\n\t_ = Target()\n}\n")

	edits := []protocol.EditOp{{Op: "delete_symbol", Path: "a.go", SymbolName: "Target"}}
	deleted, count := svc.traceDeleted(edits)
	if count != 1 || len(deleted) == 0 {
		t.Fatalf("the deleted declaration's references should be traced before the edit, got %d references for %d declarations", len(deleted), count)
	}
	if _, err := svc.Apply(protocol.ApplyRequest{Edits: edits}); err != nil {
		t.Fatal(err)
	}

	impact, files := svc.impact(edits, []string{"a.go"}, deleted, count)
	if impact.Declarations != 1 || impact.CallerFiles != 1 || strings.Join(impact.Tests, ",") != "a_test.go" {
		t.Fatalf("a deletion should report its callers and tests, got %+v", impact)
	}
	if joined := strings.Join(files, ","); !strings.Contains(joined, "b.go") || !strings.Contains(joined, "a_test.go") {
		t.Fatalf("the files a deletion breaks should be validated, got %v", files)
	}
}
func TestImpactCheckRunsDeclaredLintAfterTheTests(t *testing.T) {
	dir := t.TempDir()
	svc := newTestService(t, dir)
	registry, err := commands.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Declare("rules", commands.Command{Run: "echo 'forbidden call' >&2; exit 4", Kind: "lint"}); err != nil {
		t.Fatal(err)
	}

	outcome, summary := svc.runImpactTests(nil)
	if outcome != protocol.OutcomeFailed || !strings.Contains(summary, "declared lint rules") {
		t.Fatalf("a failing declared lint should fail the impact check and be named, got %s %q", outcome, summary)
	}

	if _, err := registry.Declare("rules", commands.Command{Run: "true", Kind: "lint"}); err != nil {
		t.Fatal(err)
	}
	if outcome, summary := svc.runImpactTests(nil); outcome != protocol.OutcomePassed {
		t.Fatalf("passing tests and lint should pass the impact check, got %s %q", outcome, summary)
	}
}

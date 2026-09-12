package code

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readBack(t *testing.T, dir string, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}

func TestReplaceTextActuallyWritesToDisk(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "main.go", "package main\n\nfunc main() {\n\tprintln(\"old\")\n}\n")

	removed, err := NewIndex(dir, nil).ReplaceTextSource("main.go", `println("old")`, `println("new")`)
	if err != nil {
		t.Fatalf("ReplaceTextSource: %v", err)
	}
	if removed != 1 {
		t.Fatalf("expected 1 replaced line counted, got %d", removed)
	}

	content := readBack(t, dir, "main.go")
	if !strings.Contains(content, `println("new")`) {
		t.Fatalf("expected the file on disk to be updated, got:\n%s", content)
	}
	if strings.Contains(content, `println("old")`) {
		t.Fatalf("expected the old text gone, got:\n%s", content)
	}
}

func TestReplaceTextRefusesAmbiguousAnchor(t *testing.T) {
	dir := t.TempDir()
	// Two identical lines: replacing "the first match" would be a coin flip
	// the caller cannot see the outcome of.
	writeGoFile(t, dir, "main.go", "package main\n\nfunc a() { x() }\n\nfunc b() { x() }\n")

	_, err := NewIndex(dir, nil).ReplaceTextSource("main.go", "x()", "y()")
	if !errors.Is(err, ErrTextAmbiguous) {
		t.Fatalf("expected ErrTextAmbiguous, got %v", err)
	}
	// The count belongs in the message so the caller knows how much context
	// to add.
	if !strings.Contains(err.Error(), "2 matches") {
		t.Fatalf("expected the match count in the error, got %v", err)
	}

	// Refusing must leave the file untouched.
	if strings.Contains(readBack(t, dir, "main.go"), "y()") {
		t.Fatalf("expected an ambiguous anchor to change nothing")
	}
}

func TestReplaceTextDisambiguatesWithMoreContext(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "main.go", "package main\n\nfunc a() { x() }\n\nfunc b() { x() }\n")

	// The documented remedy for ambiguity must actually work.
	if _, err := NewIndex(dir, nil).ReplaceTextSource("main.go", "func b() { x() }", "func b() { y() }"); err != nil {
		t.Fatalf("expected a longer anchor to resolve ambiguity, got %v", err)
	}

	content := readBack(t, dir, "main.go")
	if !strings.Contains(content, "func b() { y() }") {
		t.Fatalf("expected b to be edited, got:\n%s", content)
	}
	if !strings.Contains(content, "func a() { x() }") {
		t.Fatalf("expected a to be untouched, got:\n%s", content)
	}
}

func TestReplaceTextRefusesMissingAnchor(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "main.go", "package main\n")

	_, err := NewIndex(dir, nil).ReplaceTextSource("main.go", "nothing like this", "x")
	if !errors.Is(err, ErrTextNotFound) {
		t.Fatalf("expected ErrTextNotFound, got %v", err)
	}
}

func TestReplaceTextRejectsEmptyAnchor(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "main.go", "package main\n")

	// An empty anchor matches everywhere and nowhere; strings.Count would
	// report len+1 matches and the edit would be meaningless.
	if _, err := NewIndex(dir, nil).ReplaceTextSource("main.go", "", "x"); err == nil {
		t.Fatalf("expected an empty anchor to be rejected")
	}
}

func TestReplaceTextHandlesMultiLineAnchors(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "main.go", "package main\n\nfunc main() {\n\ta()\n\tb()\n}\n")

	removed, err := NewIndex(dir, nil).ReplaceTextSource("main.go", "\ta()\n\tb()", "\tc()")
	if err != nil {
		t.Fatalf("ReplaceTextSource: %v", err)
	}
	if removed != 2 {
		t.Fatalf("expected 2 lines counted as removed, got %d", removed)
	}
	if !strings.Contains(readBack(t, dir, "main.go"), "\tc()") {
		t.Fatalf("expected the multi-line anchor replaced")
	}
}

func TestReplaceTextSurvivesEarlierEditsShiftingLines(t *testing.T) {
	// The property that motivates the whole tool: an anchor still resolves
	// after a prior edit has moved every line below it, where a line range
	// would now point somewhere else entirely.
	dir := t.TempDir()
	writeGoFile(t, dir, "main.go", "package main\n\nfunc target() {\n\treturn\n}\n")
	index := NewIndex(dir, nil)

	if _, err := index.ReplaceTextSource("main.go", "package main\n", "package main\n\nimport \"fmt\"\n"); err != nil {
		t.Fatalf("first edit: %v", err)
	}
	if _, err := index.ReplaceTextSource("main.go", "func target() {", "func renamed() {"); err != nil {
		t.Fatalf("expected the anchor to survive the line shift, got %v", err)
	}

	content := readBack(t, dir, "main.go")
	if !strings.Contains(content, "func renamed() {") || !strings.Contains(content, `import "fmt"`) {
		t.Fatalf("expected both edits applied, got:\n%s", content)
	}
}

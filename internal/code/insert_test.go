package code

import (
	"errors"
	"strings"
	"testing"
)

func TestInsertAppendsToEndOfFile(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "main.go", "package main\n\nfunc A() {}\n")

	added, err := NewIndex(dir, nil).InsertSource("main.go", "", InsertEnd, "func B() {}\n")
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}
	if added != 1 {
		t.Fatalf("expected 1 line added, got %d", added)
	}

	content := readBack(t, dir, "main.go")
	if !strings.HasSuffix(content, "func B() {}\n") {
		t.Fatalf("expected the text appended, got:\n%q", content)
	}
	// Must not glue onto the previous line, and must not double the blank.
	if strings.Contains(content, "func A() {}func B()") || strings.Contains(content, "\n\n\n") {
		t.Fatalf("expected exactly one newline between, got:\n%q", content)
	}
}

func TestInsertBeforeAndAfterAnchor(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "main.go", "package main\n\nfunc Anchor() {}\n")
	index := NewIndex(dir, nil)

	if _, err := index.InsertSource("main.go", "func Anchor() {}", InsertBefore, "// leading comment"); err != nil {
		t.Fatalf("insert before: %v", err)
	}
	if _, err := index.InsertSource("main.go", "func Anchor() {}", InsertAfter, "// trailing comment"); err != nil {
		t.Fatalf("insert after: %v", err)
	}

	content := readBack(t, dir, "main.go")
	leading := strings.Index(content, "// leading comment")
	anchor := strings.Index(content, "func Anchor() {}")
	trailing := strings.Index(content, "// trailing comment")
	if leading < 0 || anchor < 0 || trailing < 0 {
		t.Fatalf("expected all three present, got:\n%s", content)
	}
	if !(leading < anchor && anchor < trailing) {
		t.Fatalf("expected ordering leading < anchor < trailing, got:\n%s", content)
	}
}

func TestInsertRefusesAmbiguousAnchor(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "main.go", "package main\n\nfunc a() { x() }\n\nfunc b() { x() }\n")

	_, err := NewIndex(dir, nil).InsertSource("main.go", "x()", InsertAfter, "// note")
	if !errors.Is(err, ErrTextAmbiguous) {
		t.Fatalf("expected ErrTextAmbiguous, got %v", err)
	}
	if strings.Contains(readBack(t, dir, "main.go"), "// note") {
		t.Fatalf("expected an ambiguous anchor to change nothing")
	}
}

func TestInsertRefusesMissingAnchorText(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "main.go", "package main\n")

	_, err := NewIndex(dir, nil).InsertSource("main.go", "nowhere", InsertBefore, "// note")
	if !errors.Is(err, ErrTextNotFound) {
		t.Fatalf("expected ErrTextNotFound, got %v", err)
	}
}

func TestInsertRequiresAnchorForPositionalInserts(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "main.go", "package main\n")

	_, err := NewIndex(dir, nil).InsertSource("main.go", "", InsertAfter, "// note")
	if !errors.Is(err, ErrAnchorRequired) {
		t.Fatalf("expected ErrAnchorRequired, got %v", err)
	}
}

func TestInsertRejectsUnknownPosition(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "main.go", "package main\n")

	if _, err := NewIndex(dir, nil).InsertSource("main.go", "", "sideways", "// note"); err == nil {
		t.Fatalf("expected an unknown position to be rejected")
	}
}

func TestInsertAtStartOfFile(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "main.go", "package main\n")

	if _, err := NewIndex(dir, nil).InsertSource("main.go", "", InsertStart, "// build tag"); err != nil {
		t.Fatalf("InsertSource: %v", err)
	}
	if !strings.HasPrefix(readBack(t, dir, "main.go"), "// build tag\npackage main") {
		t.Fatalf("expected the text at the very start, got:\n%q", readBack(t, dir, "main.go"))
	}
}

// Found by driving the new insert tool over MCP: an anchor supplied without a
// position was silently ignored and the text appended to the end of the file.
// An anchor is an instruction about placement — ignoring it puts code
// somewhere the caller never looked, which is the one outcome this refuses.
func TestAnchorWithoutPositionIsHonouredNotIgnored(t *testing.T) {
	out, err := insertInto("alpha\nbeta\ngamma\n", "beta", "", "inserted")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "beta\ninserted") {
		t.Fatalf("text should land after the anchor, got:\n%s", out)
	}
	if strings.HasSuffix(strings.TrimRight(out, "\n"), "inserted") {
		t.Fatalf("text was appended to the end instead of the anchor:\n%s", out)
	}
}

// The same path must still refuse an ambiguous anchor rather than falling back
// to appending — the case that exposed the bug.
func TestAmbiguousAnchorWithoutPositionIsRefused(t *testing.T) {
	_, err := insertInto("a string\nb string\nc string\n", "string", "", "x")
	if err == nil {
		t.Fatal("an anchor matching three times must be refused")
	}
	if !errors.Is(err, ErrTextAmbiguous) {
		t.Fatalf("expected an ambiguity error, got: %v", err)
	}
}

// With no anchor at all, an empty position still means append — that is the
// plain "add to the end of this file" case and must not start requiring one.
func TestNoAnchorStillAppends(t *testing.T) {
	out, err := insertInto("alpha\n", "", "", "omega")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "alpha\nomega\n" {
		t.Fatalf("expected a plain append, got %q", out)
	}
}

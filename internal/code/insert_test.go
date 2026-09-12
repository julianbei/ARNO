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

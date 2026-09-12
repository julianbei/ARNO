package code

import (
	"strings"
	"testing"
)

func TestDeleteSymbolRemovesTheWholeDeclaration(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "main.go", `package main

func Keep() int {
	return 1
}

func Remove() int {
	return 2
}

func AlsoKeep() int {
	return 3
}
`)

	symbol, removed, err := NewIndex(dir, nil).DeleteSymbolSource("main.go", "main.go::Remove@7")
	if err != nil {
		t.Fatalf("DeleteSymbolSource: %v", err)
	}
	if symbol.Name != "Remove" {
		t.Fatalf("expected the deleted symbol reported, got %q", symbol.Name)
	}
	if len(removed) == 0 {
		t.Fatalf("expected the removed lines returned")
	}

	content := readBack(t, dir, "main.go")
	if strings.Contains(content, "func Remove()") {
		t.Fatalf("expected the declaration gone, got:\n%s", content)
	}
	// The neighbours must survive intact — a brace-scanning delete that
	// overshoots is exactly the failure this tool replaces.
	if !strings.Contains(content, "func Keep()") || !strings.Contains(content, "func AlsoKeep()") {
		t.Fatalf("expected neighbouring declarations untouched, got:\n%s", content)
	}
}

func TestDeleteSymbolLeavesNoDoubleBlankLine(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "main.go", `package main

func Keep() int {
	return 1
}

func Remove() int {
	return 2
}

func AlsoKeep() int {
	return 3
}
`)

	if _, _, err := NewIndex(dir, nil).DeleteSymbolSource("main.go", "main.go::Remove@7"); err != nil {
		t.Fatalf("DeleteSymbolSource: %v", err)
	}

	// Without consuming the trailing blank, the delete leaves two blank
	// lines that gofmt then rewrites — turning a delete into a spurious diff
	// somewhere the caller never touched.
	if strings.Contains(readBack(t, dir, "main.go"), "\n\n\n") {
		t.Fatalf("expected no doubled blank line, got:\n%q", readBack(t, dir, "main.go"))
	}
}

func TestDeleteSymbolStillCompilesAfterDeletingTheLastDeclaration(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "main.go", "package main\n\nfunc Keep() int {\n\treturn 1\n}\n\nfunc Last() int {\n\treturn 2\n}\n")

	if _, _, err := NewIndex(dir, nil).DeleteSymbolSource("main.go", "main.go::Last@7"); err != nil {
		t.Fatalf("DeleteSymbolSource: %v", err)
	}

	content := readBack(t, dir, "main.go")
	if strings.Contains(content, "Last") {
		t.Fatalf("expected the last declaration gone, got:\n%s", content)
	}
	if !strings.HasSuffix(content, "}\n") {
		t.Fatalf("expected the file to still end with a newline after a closing brace, got:\n%q", content)
	}
	if !strings.Contains(content, "func Keep()") {
		t.Fatalf("expected the preceding declaration intact, got:\n%s", content)
	}
}

func TestDeleteSymbolRejectsUnknownSymbol(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "main.go", "package main\n\nfunc Keep() int { return 1 }\n")

	if _, _, err := NewIndex(dir, nil).DeleteSymbolSource("main.go", "main.go::Nope@3"); err == nil {
		t.Fatalf("expected an error for a symbol that does not exist")
	}
	if !strings.Contains(readBack(t, dir, "main.go"), "func Keep()") {
		t.Fatalf("expected a failed delete to change nothing")
	}
}

func TestExpandToSurroundingBlankNeverConsumesBothSides(t *testing.T) {
	// Eating the blank on both sides would glue the neighbouring
	// declarations together — a worse edit than leaving one blank line.
	lines := []string{"package main", "", "func A() {}", "", "func B() {}", "", "func C() {}"}

	from, to := expandToSurroundingBlank(lines, 5, 5)
	if from != 5 || to != 6 {
		t.Fatalf("expected only the trailing blank consumed, got %d-%d", from, to)
	}
}

func TestExpandToSurroundingBlankTakesLeadingBlankAtEndOfFile(t *testing.T) {
	// No trailing blank exists, so the leading one must go instead or the
	// file ends with a dangling blank line.
	lines := []string{"package main", "", "func A() {}", "", "func Last() {}"}

	from, to := expandToSurroundingBlank(lines, 5, 5)
	if from != 4 || to != 5 {
		t.Fatalf("expected the leading blank consumed at end of file, got %d-%d", from, to)
	}
}

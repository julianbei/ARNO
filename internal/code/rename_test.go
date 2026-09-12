package code

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenameRefusesWithoutGopls(t *testing.T) {
	if goplsInstalled() {
		t.Skip("gopls installed — the refusal path cannot be forced here")
	}

	dir := t.TempDir()
	writeGoFile(t, dir, "target.go", "package fixture\n\nfunc Target() int { return 1 }\n")
	writeGoFile(t, dir, "caller.go", "package fixture\n\nfunc Caller() int { return Target() }\n")

	idx := NewIndex(dir, nil)
	changed, err := idx.RenameSymbol("target.go", "target.go::Target@3", "Renamed")
	if !errors.Is(err, ErrRenameUnavailable) {
		t.Fatalf("expected ErrRenameUnavailable, got %v", err)
	}
	if changed != nil {
		t.Fatalf("expected no changed files on refusal, got %v", changed)
	}

	// The critical property: refusing must leave the workspace untouched.
	// A partially-applied rename is worse than no rename at all.
	assertFileContains(t, filepath.Join(dir, "target.go"), "func Target()")
	assertFileContains(t, filepath.Join(dir, "caller.go"), "Target()")
}

// TestRenameActuallyRewritesEveryCallSite is the whole point of 7.2: a
// rename must update files the caller never named. Only provable once gopls
// is installed.
func TestRenameActuallyRewritesEveryCallSite(t *testing.T) {
	if !goplsInstalled() {
		t.Skip("gopls not available in this environment")
	}

	dir := t.TempDir()
	writeGoFile(t, dir, "go.mod", "module fixture\n\ngo 1.21\n")
	writeGoFile(t, dir, "target.go", "package fixture\n\nfunc Target() int {\n\treturn 1\n}\n")
	writeGoFile(t, dir, "caller.go", "package fixture\n\nfunc Caller() int {\n\treturn Target() + Target()\n}\n")

	idx := NewIndex(dir, nil)
	changed, err := idx.RenameSymbol("target.go", "target.go::Target@3", "Renamed")
	if err != nil {
		t.Fatalf("RenameSymbol: %v", err)
	}

	// The caller file must be reported even though it was never named.
	var sawCaller bool
	for _, path := range changed {
		if path == "caller.go" {
			sawCaller = true
		}
	}
	if !sawCaller {
		t.Fatalf("expected caller.go in the changed set, got %v", changed)
	}

	assertFileContains(t, filepath.Join(dir, "target.go"), "func Renamed()")
	assertFileContains(t, filepath.Join(dir, "caller.go"), "Renamed() + Renamed()")
	assertFileLacks(t, filepath.Join(dir, "target.go"), "Target")
	assertFileLacks(t, filepath.Join(dir, "caller.go"), "Target")
}

func TestRenameRejectsInvalidIdentifiersBeforeTouchingDisk(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "target.go", "package fixture\n\nfunc Target() int { return 1 }\n")
	idx := NewIndex(dir, nil)

	for _, name := range []string{"", "2fast", "has-dash", "has space", "func", "type"} {
		_, err := idx.RenameSymbol("target.go", "target.go::Target@3", name)
		if !errors.Is(err, ErrInvalidIdentifier) {
			t.Fatalf("expected ErrInvalidIdentifier for %q, got %v", name, err)
		}
	}
}

func TestRenameRejectsNonGoFiles(t *testing.T) {
	dir := t.TempDir()
	source := "export function target(): number {\n\treturn 1;\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "target.ts"), []byte(source), 0o644); err != nil {
		t.Fatalf("write target.ts: %v", err)
	}

	idx := NewIndex(dir, nil)
	_, err := idx.RenameSymbol("target.ts", "target.ts::target@1", "renamed")
	if err == nil {
		t.Fatalf("expected a rename of a TypeScript symbol to be refused")
	}
	if !errors.Is(err, ErrRenameUnavailable) {
		t.Fatalf("expected ErrRenameUnavailable for a non-Go file, got %v", err)
	}
}

func TestRenameRejectsRenameToSameName(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "target.go", "package fixture\n\nfunc Target() int { return 1 }\n")

	idx := NewIndex(dir, nil)
	if _, err := idx.RenameSymbol("target.go", "target.go::Target@3", "Target"); err == nil {
		t.Fatalf("expected renaming a symbol to its current name to be rejected")
	}
}

func TestIsValidIdentifier(t *testing.T) {
	valid := []string{"X", "_x", "camelCase", "PascalCase", "with2Digits", "_"}
	for _, name := range valid {
		if !isValidIdentifier(name) {
			t.Fatalf("expected %q to be a valid identifier", name)
		}
	}
	invalid := []string{"", "2x", "a-b", "a b", "a.b", "return", "range", "interface"}
	for _, name := range invalid {
		if isValidIdentifier(name) {
			t.Fatalf("expected %q to be rejected", name)
		}
	}
}

func TestParseRenameDiffPathsReadsOnlyTheAddedSide(t *testing.T) {
	// Both "---" and "+++" name the same file; counting both would report
	// every file twice.
	diff := "--- a/internal/code/index.go\t2026-09-12 11:00:00\n" +
		"+++ b/internal/code/index.go\t2026-09-12 11:00:01\n" +
		"@@ -1,3 +1,3 @@\n" +
		"-func Old() {}\n" +
		"+func New() {}\n" +
		"--- a/internal/code/other.go\n" +
		"+++ b/internal/code/other.go\n"

	identity := func(path string) string { return path }
	paths := parseRenameDiffPaths(diff, identity)
	if len(paths) != 2 {
		t.Fatalf("expected 2 distinct paths, got %d: %v", len(paths), paths)
	}
	if paths[0] != "internal/code/index.go" || paths[1] != "internal/code/other.go" {
		t.Fatalf("expected the b/ prefix and timestamps stripped and results sorted, got %v", paths)
	}
}

func TestParseRenameDiffPathsIgnoresDevNull(t *testing.T) {
	identity := func(path string) string { return path }
	paths := parseRenameDiffPaths("--- /dev/null\n+++ /dev/null\n", identity)
	if len(paths) != 0 {
		t.Fatalf("expected /dev/null to be ignored, got %v", paths)
	}
}

func assertFileLacks(t *testing.T, path string, unwanted string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if strings.Contains(string(data), unwanted) {
		t.Fatalf("expected %s to no longer contain %q", path, unwanted)
	}
}

func assertFileContains(t *testing.T, path string, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(data), want) {
		t.Fatalf("expected %s to still contain %q", path, want)
	}
}

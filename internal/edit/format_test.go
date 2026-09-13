package edit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/julianbei/jade/internal/protocol"
)

func TestSingleEditFormatsTheTouchedFile(t *testing.T) {
	// The gap 12.7 closes: nothing else jade reports catches formatting
	// drift, because build, vet and test all pass on badly formatted code.
	dir := t.TempDir()
	svc := newTestService(t, dir)
	writeIn(t, dir, "a.go", "package main\n\nfunc A() int {\n\treturn 1\n}\n")

	response, err := svc.ReplaceText("a.go", "", "\treturn 1", "            return 1")
	if err != nil {
		t.Fatalf("ReplaceText: %v", err)
	}

	content := contentOf(t, dir, "a.go")
	if strings.Contains(content, "            return") {
		t.Fatalf("expected the file formatted after the edit, got:\n%s", content)
	}
	if len(response.Formatted) != 1 || response.Formatted[0] != "a.go" {
		t.Fatalf("expected the formatted file reported, got %v", response.Formatted)
	}
}

func TestWellFormedEditReportsNoFormatting(t *testing.T) {
	// Reporting a format that did not happen would make the field useless as
	// a signal that the agent is writing badly-shaped code.
	dir := t.TempDir()
	svc := newTestService(t, dir)
	writeIn(t, dir, "a.go", "package main\n\nfunc A() int {\n\treturn 1\n}\n")

	response, err := svc.ReplaceText("a.go", "", "\treturn 1", "\treturn 2")
	if err != nil {
		t.Fatalf("ReplaceText: %v", err)
	}
	if len(response.Formatted) != 0 {
		t.Fatalf("expected no formatting for already-clean code, got %v", response.Formatted)
	}
}

func TestCreateFileIsFormatted(t *testing.T) {
	dir := t.TempDir()
	svc := newTestService(t, dir)

	response, err := svc.CreateFile("new.go", "package main\n\nfunc New()     int {\n              return 1\n}\n")
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	if len(response.Formatted) != 1 {
		t.Fatalf("expected the new file formatted, got %v", response.Formatted)
	}
	if strings.Contains(contentOf(t, dir, "new.go"), "              return") {
		t.Fatalf("expected gofmt to normalize the new file")
	}
}

func TestDeleteSymbolFormatsRemainder(t *testing.T) {
	dir := t.TempDir()
	svc := newTestService(t, dir)
	writeIn(t, dir, "a.go", "package main\n\nfunc Keep() int {\n\treturn 1\n}\n\nfunc Drop() int {\n\treturn 2\n}\n")

	if _, err := svc.DeleteSymbol("a.go", "a.go::Drop@7", ""); err != nil {
		t.Fatalf("DeleteSymbol: %v", err)
	}
	// gofmt would rewrite a file left with trailing blank lines; a clean
	// delete means it has nothing to do.
	if strings.Contains(contentOf(t, dir, "a.go"), "\n\n\n") {
		t.Fatalf("expected no stray blank lines after delete+format")
	}
}

func TestFormattingDoesNotFailTheEditWhenFileIsBroken(t *testing.T) {
	// gofmt refuses to format a file it cannot parse. The edit itself still
	// succeeded and must be reported as such — the diagnostics are what tell
	// the caller it is broken.
	dir := t.TempDir()
	svc := newTestService(t, dir)
	writeIn(t, dir, "a.go", "package main\n\nfunc A() int {\n\treturn 1\n}\n")

	response, err := svc.ReplaceText("a.go", "", "return 1", "return (((")
	if err != nil {
		t.Fatalf("expected the edit to succeed even though the result cannot be formatted: %v", err)
	}
	if len(response.Formatted) != 0 {
		t.Fatalf("expected no formatting reported for an unparseable file, got %v", response.Formatted)
	}
	if !strings.Contains(contentOf(t, dir, "a.go"), "return (((") {
		t.Fatalf("expected the edit itself to have landed")
	}
}

func TestFormatterSelectionByExtension(t *testing.T) {
	dir := t.TempDir()
	if chosen, ok := formatterFor(dir, "a.go"); !ok || chosen.name != "gofmt" {
		t.Fatalf("expected gofmt for Go, got %+v", chosen)
	}
	// A project that declares no formatter gets none, even for languages
	// that have one — running one unattended could reformat far more than
	// the agent touched.
	for _, path := range []string{"a.ts", "a.tsx", "a.py", "a.scala", "a.rb", "a.java", "a.md", "a.json", "Makefile"} {
		if chosen, ok := formatterFor(dir, path); ok {
			t.Fatalf("expected no formatter for %s in an undeclared project, got %+v", path, chosen)
		}
	}
}

// fakeFormatter writes an executable that replaces the file's content with a
// marker, so a test can tell whether it ran and on what.
func fakeFormatter(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nfor last; do :; done\nprintf 'formatted\\n' > \"$last\"\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestPrettierRunsOnlyWhenTheProjectDeclaresIt(t *testing.T) {
	dir := t.TempDir()
	writeIn(t, dir, "web/src/a.ts", "const   a=1\n")
	fakeFormatter(t, filepath.Join(dir, "web", "node_modules", ".bin", "prettier"))

	if formatted := formatFiles(dir, []string{"web/src/a.ts"}); len(formatted) != 0 {
		t.Fatalf("prettier ran with no project config: %v", formatted)
	}

	writeIn(t, dir, "web/.prettierrc", "{}\n")
	if formatted := formatFiles(dir, []string{"web/src/a.ts"}); len(formatted) != 1 {
		t.Fatalf("expected prettier to run once the nested project declares it, got %v", formatted)
	}
	if contentOf(t, dir, "web/src/a.ts") != "formatted\n" {
		t.Fatalf("expected the project's own prettier to have rewritten the file")
	}
}

func TestPrettierKeyInPackageJSONCountsAsDeclared(t *testing.T) {
	dir := t.TempDir()
	writeIn(t, dir, "package.json", `{"name":"x","prettier":{"semi":false}}`)
	fakeFormatter(t, filepath.Join(dir, "node_modules", ".bin", "prettier"))

	if chosen, ok := formatterFor(dir, "a.js"); !ok || chosen.name != "prettier" {
		t.Fatalf("expected prettier from package.json's prettier key, got %+v", chosen)
	}
}

func TestPrettierConfigWithoutAnInstalledBinaryIsSkipped(t *testing.T) {
	// A global prettier is a different version from the one the project
	// pinned, and would reformat to different rules.
	dir := t.TempDir()
	writeIn(t, dir, ".prettierrc", "{}\n")
	if chosen, ok := formatterFor(dir, "a.ts"); ok {
		t.Fatalf("expected no formatter without the project's own prettier, got %+v", chosen)
	}
}

func TestPythonFormatterFollowsPyproject(t *testing.T) {
	dir := t.TempDir()
	fakeFormatter(t, filepath.Join(dir, ".venv", "bin", "black"))
	fakeFormatter(t, filepath.Join(dir, ".venv", "bin", "ruff"))

	writeIn(t, dir, "pyproject.toml", "[tool.ruff]\nline-length = 100\n")
	if chosen, ok := formatterFor(dir, "a.py"); ok {
		t.Fatalf("ruff as a linter only is not a formatter choice, got %+v", chosen)
	}

	writeIn(t, dir, "pyproject.toml", "[tool.black]\nline-length = 100\n")
	if chosen, ok := formatterFor(dir, "a.py"); !ok || chosen.name != "black" {
		t.Fatalf("expected black, got %+v", chosen)
	}

	writeIn(t, dir, "pyproject.toml", "[tool.ruff.format]\nquote-style = \"single\"\n")
	chosen, ok := formatterFor(dir, "a.py")
	if !ok || chosen.name != "ruff" || chosen.args[0] != "format" {
		t.Fatalf("expected ruff format, got %+v", chosen)
	}
}

func TestScalafmtNeedsItsConfig(t *testing.T) {
	dir := t.TempDir()
	bin := t.TempDir()
	fakeFormatter(t, filepath.Join(bin, "scalafmt"))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	if chosen, ok := formatterFor(dir, "A.scala"); ok {
		t.Fatalf("scalafmt without .scalafmt.conf, got %+v", chosen)
	}
	writeIn(t, dir, ".scalafmt.conf", "version = 3.8.0\n")
	if chosen, ok := formatterFor(dir, "A.scala"); !ok || chosen.name != "scalafmt" {
		t.Fatalf("expected scalafmt with its config, got %+v", chosen)
	}
}

func TestFormatterConfigAboveTheWorkspaceIsIgnored(t *testing.T) {
	// A repository checked out inside a directory that happens to declare a
	// formatter must not inherit it.
	outer := t.TempDir()
	writeIn(t, outer, ".prettierrc", "{}\n")
	fakeFormatter(t, filepath.Join(outer, "node_modules", ".bin", "prettier"))
	root := filepath.Join(outer, "repo")
	writeIn(t, root, "a.ts", "x\n")

	if chosen, ok := formatterFor(root, "a.ts"); ok {
		t.Fatalf("inherited a formatter from outside the workspace: %+v", chosen)
	}
}

func TestFormatFilesIgnoresUnknownExtensions(t *testing.T) {
	dir := t.TempDir()
	writeIn(t, dir, "notes.md", "#  badly   spaced\n")

	if formatted := formatFiles(dir, []string{"notes.md"}); len(formatted) != 0 {
		t.Fatalf("expected no formatting attempted, got %v", formatted)
	}
}

var _ = protocol.EditResponse{}

func TestReplaceFileOverwritesWholesale(t *testing.T) {
	// The case no anchored edit can express: the document now says something
	// else, so there is nothing left to anchor on.
	dir := t.TempDir()
	svc := newTestService(t, dir)
	writeIn(t, dir, "notes.md", "# Old\n\nEverything here is replaced.\n")

	if _, err := svc.ReplaceFile("notes.md", "# New\n\nCompletely different.\n"); err != nil {
		t.Fatalf("ReplaceFile: %v", err)
	}

	got := contentOf(t, dir, "notes.md")
	if strings.Contains(got, "Old") {
		t.Fatalf("expected the old content gone, got:\n%s", got)
	}
	if !strings.Contains(got, "Completely different") {
		t.Fatalf("expected the new content, got:\n%s", got)
	}
}

func TestReplaceFileReportsWhatItDestroyed(t *testing.T) {
	// The size of what was destroyed is the one fact a caller cannot recover
	// from the response otherwise.
	dir := t.TempDir()
	svc := newTestService(t, dir)
	writeIn(t, dir, "notes.md", "one\ntwo\nthree\nfour\n")

	response, err := svc.ReplaceFile("notes.md", "just one line\n")
	if err != nil {
		t.Fatalf("ReplaceFile: %v", err)
	}
	if response.RemovedLines != 4 {
		t.Fatalf("expected 4 removed lines, got %d", response.RemovedLines)
	}
	if response.AddedLines != 1 {
		t.Fatalf("expected 1 added line, got %d", response.AddedLines)
	}
}

func TestReplaceFileRefusesToCreate(t *testing.T) {
	// The symmetry that removes the need for a force flag: create_file refuses
	// when the file exists, this refuses when it does not, so neither can do
	// the other's job by accident.
	dir := t.TempDir()
	svc := newTestService(t, dir)

	_, err := svc.ReplaceFile("nothing-here.md", "content")
	if err == nil {
		t.Fatalf("expected a refusal for a file that does not exist")
	}
	if !strings.Contains(err.Error(), "create_file") {
		t.Fatalf("expected the error to name the right tool, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "nothing-here.md")); statErr == nil {
		t.Fatalf("expected no file to be created by a refused replace")
	}
}

func TestReplaceFileBumpsTheRevision(t *testing.T) {
	dir := t.TempDir()
	svc := newTestService(t, dir)
	writeIn(t, dir, "a.go", "package main\n\nfunc A() {}\n")

	response, err := svc.ReplaceFile("a.go", "package main\n\nfunc B() {}\n")
	if err != nil {
		t.Fatalf("ReplaceFile: %v", err)
	}
	if response.OldRevision == response.NewRevision {
		t.Fatalf("expected the revision to move, stayed at %s", response.OldRevision)
	}
	if len(response.Changed) != 1 || response.Changed[0] != "a.go" {
		t.Fatalf("expected the path reported as changed, got %v", response.Changed)
	}
}

func TestReplaceFileFormatsTheResult(t *testing.T) {
	// It goes through the same edit path as everything else, so a badly shaped
	// rewrite is cleaned up rather than landing as written.
	dir := t.TempDir()
	svc := newTestService(t, dir)
	writeIn(t, dir, "a.go", "package main\n\nfunc A() {}\n")

	response, err := svc.ReplaceFile("a.go", "package main\n\nfunc A() int {\n              return 1\n}\n")
	if err != nil {
		t.Fatalf("ReplaceFile: %v", err)
	}
	if strings.Contains(contentOf(t, dir, "a.go"), "              return") {
		t.Fatalf("expected gofmt to normalize the rewrite")
	}
	if len(response.Formatted) != 1 {
		t.Fatalf("expected the formatting reported, got %v", response.Formatted)
	}
}

func TestReplaceFileAddsATrailingNewline(t *testing.T) {
	// Matches CreateFile's behaviour: a file without a final newline is a
	// diff-noise generator, and the two tools must not disagree about it.
	dir := t.TempDir()
	svc := newTestService(t, dir)
	writeIn(t, dir, "notes.md", "old\n")

	if _, err := svc.ReplaceFile("notes.md", "no trailing newline"); err != nil {
		t.Fatalf("ReplaceFile: %v", err)
	}
	if !strings.HasSuffix(contentOf(t, dir, "notes.md"), "\n") {
		t.Fatalf("expected a trailing newline to be added")
	}
}

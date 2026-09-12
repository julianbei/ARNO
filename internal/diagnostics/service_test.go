package diagnostics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/julianbei/jade/internal/protocol"
)

func TestImmediateDetectsRealSyntaxErrors(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "broken.go")
	if err := os.WriteFile(path, []byte("package main\n\nfunc Broken() string {\n\treturn \"unterminated\n}\n"), 0o644); err != nil {
		t.Fatalf("write broken file: %v", err)
	}

	svc := NewService(root)
	diagnostics := svc.Immediate("broken.go")
	if len(diagnostics) == 0 {
		t.Fatalf("expected at least one diagnostic for a syntax error, got none")
	}
	if diagnostics[0].Level != protocol.DiagnosticError {
		t.Fatalf("expected error-level diagnostic, got %v", diagnostics[0].Level)
	}
	if diagnostics[0].Line == 0 {
		t.Fatalf("expected a line number for the syntax error, got %#v", diagnostics[0])
	}
}

func TestImmediateReturnsNoDiagnosticsForCleanGoFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "clean.go")
	if err := os.WriteFile(path, []byte("package main\n\nfunc Clean() string {\n\treturn \"ok\"\n}\n"), 0o644); err != nil {
		t.Fatalf("write clean file: %v", err)
	}

	svc := NewService(root)
	// Asserts emptiness, not nil: when gopls is available it runs and
	// returns an empty (non-nil) slice for a clean file, while the
	// parse-only path returns nil. Both mean "no problems found", so the
	// test must not depend on which one answered.
	if diagnostics := svc.Immediate("clean.go"); len(diagnostics) != 0 {
		t.Fatalf("expected no diagnostics for syntactically valid Go, got %#v", diagnostics)
	}
}

func TestImmediateResolvesSymbolIDScopeToItsFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "broken.go")
	if err := os.WriteFile(path, []byte("package main\n\nfunc Broken() string {\n\treturn \"unterminated\n}\n"), 0o644); err != nil {
		t.Fatalf("write broken file: %v", err)
	}

	svc := NewService(root)
	diagnostics := svc.Immediate("broken.go::Broken@3")
	if len(diagnostics) == 0 {
		t.Fatalf("expected diagnostics when scope is a symbol ID, got none")
	}
	if diagnostics[0].Path != "broken.go" {
		t.Fatalf("expected diagnostic path to be the resolved file, got %q", diagnostics[0].Path)
	}
}

func TestImmediateSkipsNonGoFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes.md")
	if err := os.WriteFile(path, []byte("not go code at all {{{"), 0o644); err != nil {
		t.Fatalf("write markdown file: %v", err)
	}

	svc := NewService(root)
	if diagnostics := svc.Immediate("notes.md"); diagnostics != nil {
		t.Fatalf("expected no diagnostics for a non-Go file, got %#v", diagnostics)
	}
}

func TestDecisiveLinesPrefersTheEndOfOutputOverTheStart(t *testing.T) {
	output := "ERROR: irrelevant setup warning from long ago\n" +
		strings.Repeat("ok   some/package/path   0.01s\n", 50) +
		"--- FAIL: TestSomething (0.00s)\n" +
		"    panic: real failure at the end\n"

	lines := decisiveLines(output)
	if len(lines) == 0 {
		t.Fatalf("expected decisive lines, got none")
	}
	last := lines[len(lines)-1]
	if !strings.Contains(last, "real failure at the end") {
		t.Fatalf("expected the last decisive line to be the actual failure near the end of output, got %q", last)
	}
}

func TestDecisiveLinesExcludesWarnings(t *testing.T) {
	output := "warning: unused variable x (this looks like an error but is not)\n" +
		"FAIL: TestReal (0.00s)\n"

	lines := decisiveLines(output)
	for _, line := range lines {
		if strings.HasPrefix(strings.ToLower(line), "warning") {
			t.Fatalf("expected warning lines to be excluded, got %q in %#v", line, lines)
		}
	}
	found := false
	for _, line := range lines {
		if strings.Contains(line, "FAIL: TestReal") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the real failure line to survive, got %#v", lines)
	}
}

func TestDecisiveLinesDedupesRepeatedLines(t *testing.T) {
	output := "panic: same error\ngoroutine 1\npanic: same error\ngoroutine 2\npanic: same error\n"

	lines := decisiveLines(output)
	count := 0
	for _, line := range lines {
		if line == "panic: same error" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected the repeated panic line to be deduped to 1 occurrence, got %d in %#v", count, lines)
	}
}

func TestDecisiveLinesCapsAtEightLines(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 20; i++ {
		b.WriteString("FAIL: TestCase")
		b.WriteString(strings.Repeat("x", i+1))
		b.WriteString("\n")
	}

	lines := decisiveLines(b.String())
	if len(lines) > 8 {
		t.Fatalf("expected at most 8 decisive lines, got %d", len(lines))
	}
}

func TestDecisiveSummaryFindsACompilerErrorBuriedInTheMiddle(t *testing.T) {
	// The exact case that exposed this during 8.1: a long log whose only
	// real failure sits in the middle. Before 12.6 no marker matched, so the
	// summary fell back to the first three lines — pure filler.
	var b strings.Builder
	for i := 0; i < 2000; i++ {
		b.WriteString("filler output line with nothing interesting in it\n")
	}
	b.WriteString("main.go:42:3: undefined: decisiveMiddleSymbol\n")
	for i := 0; i < 2000; i++ {
		b.WriteString("filler output line with nothing interesting in it\n")
	}

	summary := DecisiveSummary(b.String())
	if !strings.Contains(summary, "decisiveMiddleSymbol") {
		t.Fatalf("expected the buried compiler error to be found, got %q", summary)
	}
	if strings.Contains(summary, "filler output line") {
		t.Fatalf("expected filler excluded once a real diagnostic exists, got %q", summary)
	}
}

func TestDecisiveLinesRecognizesCompilerDiagnosticForms(t *testing.T) {
	cases := []string{
		"main.go:42:3: undefined: X",
		"internal/code/index.go:17:2: imported and not used",
		"src/app.ts:12:5: Type 'string' is not assignable",
		"src/lib.rs:7:1: expected one of",
	}
	for _, line := range cases {
		if lines := decisiveLines(line + "\n"); len(lines) != 1 {
			t.Fatalf("expected %q to be recognized as decisive, got %#v", line, lines)
		}
	}
}

func TestDecisiveLinesDoesNotTreatOrdinaryColonsAsDiagnostics(t *testing.T) {
	// A path-like prefix is required; prose with colons and digits must not
	// masquerade as a compiler diagnostic or every log line would match.
	notDiagnostics := []string{
		"ok  \tgithub.com/julianbei/jade/internal/code\t0.248s",
		"Compiling thing v0.1.0",
		"note: run with RUST_BACKTRACE=1",
		"12:34:56 starting build",
	}
	for _, line := range notDiagnostics {
		if lines := decisiveLines(line + "\n"); len(lines) != 0 {
			t.Fatalf("expected %q not to be decisive, got %#v", line, lines)
		}
	}
}

func TestDecisiveSummaryFallsBackToTheEndNotTheStart(t *testing.T) {
	// Nothing looks like a failure. The conclusion of a tool's output is at
	// the end; the start is setup noise.
	var b strings.Builder
	b.WriteString("starting up\n")
	for i := 0; i < 500; i++ {
		b.WriteString("Compiling package number filler\n")
	}
	b.WriteString("Done in 1.4s\n")

	summary := DecisiveSummary(b.String())
	if !strings.Contains(summary, "Done in 1.4s") {
		t.Fatalf("expected the tail of the output, got %q", summary)
	}
	if strings.Contains(summary, "starting up") {
		t.Fatalf("expected the head of a long log to be dropped, got %q", summary)
	}
}

func TestDecisiveSummaryStillReportsNoOutput(t *testing.T) {
	if got := DecisiveSummary("   \n\n"); got != "no output" {
		t.Fatalf("expected \"no output\", got %q", got)
	}
}

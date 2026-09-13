package diagnostics

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/julianbei/jade/internal/protocol"
)

func write(t *testing.T, root string, name string, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// An empty diagnostic list must never be the whole answer for a Go file: the
// response has to say whether gopls or only the parser looked.
func TestGoCheckNamesTheCheckerThatRan(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.go", "package main\n\nfunc A() {}\n")
	write(t, root, "broken.go", "package main\n\nfunc (\n")

	service := NewService(root)

	clean := service.Check("a.go")
	if clean.Checker == "" || clean.Unchecked != "" {
		t.Fatalf("expected a named checker for a clean Go file, got %+v", clean)
	}
	broken := service.Check("broken.go")
	if broken.Checker != "go/parser" || len(broken.Diagnostics) == 0 {
		t.Fatalf("expected go/parser to report the syntax error, got %+v", broken)
	}
}

func TestLanguageServerResultsAreReported(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.ts", "const a: number = 'x'\n")
	write(t, root, "b.py", "x = 1\n")

	service := NewService(root)
	service.UseLanguageServers(func(path string) ([]protocol.Diagnostic, string, string, bool) {
		switch path {
		case "a.ts":
			return []protocol.Diagnostic{{Level: protocol.DiagnosticError, Path: path, Line: 1, Message: "type mismatch"}},
				"typescript-language-server", "", true
		case "b.py":
			return nil, "", "pyright-langserver is not installed", true
		}
		return nil, "", "", false
	})

	if got := service.Immediate("a.ts"); len(got) != 1 || got[0].Message != "type mismatch" {
		t.Fatalf("expected the server's diagnostic, got %+v", got)
	}
	report := service.Checks("a.ts", "b.py")
	if !reflect.DeepEqual(report.Checked, []string{"typescript-language-server"}) {
		t.Fatalf("checked = %v", report.Checked)
	}
	if len(report.Unchecked) != 1 || report.Unchecked[0] != "b.py: pyright-langserver is not installed" {
		t.Fatalf("unchecked = %v", report.Unchecked)
	}
}

// "not checked" on every Markdown edit would be the same noise the drift
// count was.
func TestFilesWithNoLanguageReportNothing(t *testing.T) {
	root := t.TempDir()
	write(t, root, "notes.md", "# notes\n")

	service := NewService(root)
	service.UseLanguageServers(func(string) ([]protocol.Diagnostic, string, string, bool) {
		return nil, "", "", false
	})
	if report := service.Checks("notes.md"); len(report.Checked) != 0 || len(report.Unchecked) != 0 {
		t.Fatalf("expected an empty report for a file with no language, got %+v", report)
	}
}

// Edit responses ask for diagnostics and for the report separately. Waiting on
// a language server twice for one unchanged file would double every edit.
func TestCheckIsNotRepeatedForAnUnchangedFile(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.ts", "let a = 1\n")

	calls := 0
	service := NewService(root)
	service.UseLanguageServers(func(path string) ([]protocol.Diagnostic, string, string, bool) {
		calls++
		return nil, "tsserver", "", true
	})

	service.Immediate("a.ts")
	service.Checks("a.ts")
	if calls != 1 {
		t.Fatalf("expected one check for an unchanged file, got %d", calls)
	}

	write(t, root, "a.ts", "let a = 12345\n")
	service.Checks("a.ts")
	if calls != 2 {
		t.Fatalf("expected a fresh check after the file changed, got %d calls", calls)
	}
}

func TestSymbolScopesResolveToTheirFile(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.ts", "function f() {}\n")

	var seen []string
	service := NewService(root)
	service.UseLanguageServers(func(path string) ([]protocol.Diagnostic, string, string, bool) {
		seen = append(seen, path)
		return nil, "tsserver", "", true
	})
	service.Checks("a.ts::f@1")
	if len(seen) != 1 || !strings.HasSuffix(seen[0], "a.ts") || strings.Contains(seen[0], "::") {
		t.Fatalf("expected the symbol scope checked as its file, got %v", seen)
	}
}

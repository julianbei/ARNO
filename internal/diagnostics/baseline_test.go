package diagnostics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/julianbei/arno/internal/protocol"
	"github.com/julianbei/arno/internal/writes"
)

// markerChecker is a stand-in language server: one error per line that
// contains BAD, with the marker's suffix as the message, so the test can
// tell findings apart.
func markerChecker(root string) LanguageServerCheck {
	return func(path string) ([]protocol.Diagnostic, string, string, bool) {
		if !strings.HasSuffix(path, ".py") {
			return nil, "", "", false
		}
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			return nil, "fake", "", true
		}
		var out []protocol.Diagnostic
		for i, line := range strings.Split(string(data), "\n") {
			if at := strings.Index(line, "BAD"); at >= 0 {
				out = append(out, protocol.Diagnostic{Level: protocol.DiagnosticError, Path: path, Line: i + 1, Column: at + 1, Message: strings.TrimSpace(line[at:])})
			}
		}
		return out, "fake", "", true
	}
}

// An edit response carries the diagnostics the edit caused, not the ones the
// file already had; those are counted, once, in the check report.
func TestImmediateLeavesOutPreexistingDiagnostics(t *testing.T) {
	root := t.TempDir()
	service := NewService(root)
	service.UseLanguageServers(markerChecker(root))
	path := filepath.Join(root, "models.py")
	if err := os.WriteFile(path, []byte("x = 1\nBAD one\nBAD two\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Never checked before: the write path snapshots the file first.
	if err := writes.File(path, []byte("# moved\nx = 1\nBAD one\nBAD two\nBAD three\n")); err != nil {
		t.Fatal(err)
	}
	fresh := service.Immediate("models.py")
	report := service.Checks("models.py")
	if len(fresh) != 1 || fresh[0].Message != "BAD three" || fresh[0].Line != 5 {
		t.Fatalf("expected only the new finding at its new line, got %+v", fresh)
	}
	if report.Preexisting != 2 || strings.Join(report.Checked, ",") != "fake" {
		t.Fatalf("expected 2 pre-existing findings counted and the checker named, got %+v", report)
	}

	// The count is consumed with the response.
	if again := service.Checks("models.py"); again.Preexisting != 0 {
		t.Fatalf("expected the count consumed, got %+v", again)
	}

	// A second edit starts from the previous response's state: one fixed,
	// nothing new. Two writes in one edit (an op and its formatter) keep the
	// first baseline.
	if err := writes.File(path, []byte("x = 1\nBAD one\nBAD three\n")); err != nil {
		t.Fatal(err)
	}
	if err := writes.File(path, []byte("x = 1\nBAD one\n\nBAD three\n")); err != nil {
		t.Fatal(err)
	}
	fresh = service.Immediate("models.py")
	report = service.Checks("models.py")
	if len(fresh) != 0 || report.Preexisting != 2 {
		t.Fatalf("expected nothing new and 2 pre-existing, got %+v / %+v", fresh, report)
	}

	// A file created by the edit has no baseline findings: everything is new.
	created := filepath.Join(root, "new.py")
	if err := writes.File(created, []byte("BAD fresh\n")); err != nil {
		t.Fatal(err)
	}
	fresh = service.Immediate("new.py")
	if len(fresh) != 1 || service.Checks("new.py").Preexisting != 0 {
		t.Fatalf("expected the new file's finding reported as new, got %+v", fresh)
	}
}

// Without a write through the write path there is no baseline, and the
// response reports everything, as before.
func TestImmediateWithoutBaselineReportsEverything(t *testing.T) {
	root := t.TempDir()
	service := NewService(root)
	service.UseLanguageServers(markerChecker(root))
	if err := os.WriteFile(filepath.Join(root, "a.py"), []byte("BAD one\nBAD two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if fresh := service.Immediate("a.py"); len(fresh) != 2 {
		t.Fatalf("expected both findings, got %+v", fresh)
	}
	if report := service.Checks("a.py"); report.Preexisting != 0 {
		t.Fatalf("expected no pre-existing count without a baseline, got %+v", report)
	}
}

// A checker that could not look before the write — a language server still
// starting — leaves no baseline, so the response reports everything rather
// than claiming the file had nothing before.
func TestUncheckedSnapshotLeavesNoBaseline(t *testing.T) {
	root := t.TempDir()
	service := NewService(root)
	calls := 0
	service.UseLanguageServers(func(path string) ([]protocol.Diagnostic, string, string, bool) {
		calls++
		if calls == 1 {
			return nil, "", "fake is still starting", true
		}
		return []protocol.Diagnostic{{Level: protocol.DiagnosticError, Path: path, Line: 1, Message: "old"}}, "fake", "", true
	})
	path := filepath.Join(root, "a.py")
	if err := os.WriteFile(path, []byte("BAD old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writes.File(path, []byte("BAD old\n# edited\n")); err != nil {
		t.Fatal(err)
	}
	fresh := service.Immediate("a.py")
	report := service.Checks("a.py")
	if len(fresh) != 1 || report.Preexisting != 0 {
		t.Fatalf("expected everything reported with no pre-existing count, got %+v / %+v", fresh, report)
	}
}

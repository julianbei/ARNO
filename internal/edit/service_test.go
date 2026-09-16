package edit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/julianbei/arno/internal/code"
	"github.com/julianbei/arno/internal/diagnostics"
	"github.com/julianbei/arno/internal/jobs"
	"github.com/julianbei/arno/internal/workspace"
)

func newTestService(t *testing.T, root string) *Service {
	t.Helper()
	return NewService(
		workspace.NewManager(root, nil),
		code.NewIndex(root, nil),
		diagnostics.NewService(root),
		jobs.NewRunner(nil),
	)
}

func TestReplaceSymbolRejectsStaleRevision(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "greeter.go")
	if err := os.WriteFile(path, []byte("package main\n\nfunc Greet() string {\n\treturn \"hi\"\n}\n"), 0o644); err != nil {
		t.Fatalf("write greeter file: %v", err)
	}

	svc := newTestService(t, root)

	symbols, err := code.NewIndex(root, nil).Outline("greeter.go")
	if err != nil {
		t.Fatalf("outline: %v", err)
	}
	var symbolID string
	for _, symbol := range symbols {
		if symbol.Name == "Greet" {
			symbolID = symbol.ID
		}
	}
	if symbolID == "" {
		t.Fatalf("expected to find Greet symbol, got %#v", symbols)
	}

	_, err = svc.ReplaceSymbol(symbolID, "r99", "func Greet() string {\n\treturn \"hello\"\n}\n")
	if !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("expected ErrStaleRevision, got %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back file: %v", err)
	}
	if !strings.Contains(string(data), "\"hi\"") || strings.Contains(string(data), "hello") {
		t.Fatalf("expected file untouched after rejected edit, got %q", string(data))
	}
}

func TestReplaceSymbolAcceptsMatchingRevision(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "greeter.go")
	if err := os.WriteFile(path, []byte("package main\n\nfunc Greet() string {\n\treturn \"hi\"\n}\n"), 0o644); err != nil {
		t.Fatalf("write greeter file: %v", err)
	}

	svc := newTestService(t, root)

	symbols, err := code.NewIndex(root, nil).Outline("greeter.go")
	if err != nil {
		t.Fatalf("outline: %v", err)
	}
	var symbolID string
	for _, symbol := range symbols {
		if symbol.Name == "Greet" {
			symbolID = symbol.ID
		}
	}
	if symbolID == "" {
		t.Fatalf("expected to find Greet symbol, got %#v", symbols)
	}

	current := svc.workspace.Revision()
	if _, err := svc.ReplaceSymbol(symbolID, current, "func Greet() string {\n\treturn \"hello\"\n}\n"); err != nil {
		t.Fatalf("replace symbol with matching revision: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back file: %v", err)
	}
	if !strings.Contains(string(data), "hello") {
		t.Fatalf("expected file to contain new implementation, got %q", string(data))
	}
}

func TestReplaceSymbolReportsRealDiffCountsForSameSizeSwap(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "greeter.go")
	if err := os.WriteFile(path, []byte("package main\n\nfunc Greet() string {\n\treturn \"hi\"\n}\n"), 0o644); err != nil {
		t.Fatalf("write greeter file: %v", err)
	}

	svc := newTestService(t, root)

	symbols, err := code.NewIndex(root, nil).Outline("greeter.go")
	if err != nil {
		t.Fatalf("outline: %v", err)
	}
	var symbolID string
	for _, symbol := range symbols {
		if symbol.Name == "Greet" {
			symbolID = symbol.ID
		}
	}
	if symbolID == "" {
		t.Fatalf("expected to find Greet symbol, got %#v", symbols)
	}

	current := svc.workspace.Revision()
	resp, err := svc.ReplaceSymbol(symbolID, current, "func Greet() string {\n\treturn \"hello\"\n}\n")
	if err != nil {
		t.Fatalf("replace symbol: %v", err)
	}

	// Regression: this used to hardcode AddedLines=countLines(newCode) and
	// RemovedLines=0 regardless of the old body, so a same-size 3-line swap
	// reported "+3/-0" instead of the real "+3/-3".
	if resp.AddedLines != 3 {
		t.Fatalf("expected AddedLines=3, got %d", resp.AddedLines)
	}
	if resp.RemovedLines != 3 {
		t.Fatalf("expected RemovedLines=3 (not the old hardcoded 0), got %d", resp.RemovedLines)
	}
}

// A single edit starts no background job: its result was never shown, and
// diagnostics already come back inline.
func TestReplaceSymbolStartsNoBackgroundJob(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "greeter.go")
	if err := os.WriteFile(path, []byte("package main\n\nfunc Greet() string {\n\treturn \"hi\"\n}\n"), 0o644); err != nil {
		t.Fatalf("write greeter file: %v", err)
	}

	svc := newTestService(t, root)

	symbols, err := code.NewIndex(root, nil).Outline("greeter.go")
	if err != nil {
		t.Fatalf("outline: %v", err)
	}
	var symbolID string
	for _, symbol := range symbols {
		if symbol.Name == "Greet" {
			symbolID = symbol.ID
		}
	}
	if symbolID == "" {
		t.Fatalf("expected to find Greet symbol, got %#v", symbols)
	}

	current := svc.workspace.Revision()
	resp, err := svc.ReplaceSymbol(symbolID, current, "func Greet() string {\n\treturn \"hello\"\n}\n")
	if err != nil {
		t.Fatalf("replace symbol: %v", err)
	}
	if len(resp.Jobs) != 0 {
		t.Fatalf("expected no background job, got %#v", resp.Jobs)
	}
}

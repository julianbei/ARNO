package diagnostics

import (
	"testing"

	"github.com/julianbei/jade/internal/toolchain"
)

func TestParseGoplsCheckOutputExtractsDiagnostics(t *testing.T) {
	output := "session.go:12:3: declared and not used: token\n" +
		"session.go:20:8-14: undefined: Refresh\n" +
		"this line does not match the expected format at all\n"

	diagnostics := parseGoplsCheckOutput("session.go", output)
	if len(diagnostics) != 2 {
		t.Fatalf("expected 2 diagnostics, got %d: %#v", len(diagnostics), diagnostics)
	}

	if diagnostics[0].Line != 12 || diagnostics[0].Column != 3 {
		t.Fatalf("expected first diagnostic at 12:3, got %#v", diagnostics[0])
	}
	if diagnostics[0].Message != "declared and not used: token" {
		t.Fatalf("expected message to be extracted, got %q", diagnostics[0].Message)
	}

	if diagnostics[1].Line != 20 || diagnostics[1].Column != 8 {
		t.Fatalf("expected second diagnostic to parse the range form 8-14 as column 8, got %#v", diagnostics[1])
	}
	if diagnostics[1].Message != "undefined: Refresh" {
		t.Fatalf("expected message to be extracted, got %q", diagnostics[1].Message)
	}
}

func TestParseGoplsCheckOutputReturnsEmptyForCleanOutput(t *testing.T) {
	diagnostics := parseGoplsCheckOutput("session.go", "")
	if len(diagnostics) != 0 {
		t.Fatalf("expected no diagnostics for empty output, got %#v", diagnostics)
	}
}

func TestGoplsCheckReturnsFalseWhenGoplsIsUnavailable(t *testing.T) {
	if _, ok := toolchain.Gopls(); ok {
		t.Skip("gopls is installed in this environment; this test exercises the not-installed path")
	}

	root := t.TempDir()
	diagnostics, ran := goplsCheck(root, "anything.go")
	if ran {
		t.Fatalf("expected ran=false when gopls is not on PATH, got diagnostics=%#v", diagnostics)
	}
	if diagnostics != nil {
		t.Fatalf("expected nil diagnostics when gopls did not run, got %#v", diagnostics)
	}
}

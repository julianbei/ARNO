package render

import (
	"strings"
	"testing"

	"github.com/julianbei/arno/internal/protocol"
)

func TestFormatCandidatesEmptyIsEmptyString(t *testing.T) {
	if got := FormatCandidates("Detected candidates:", nil); got != "" {
		t.Fatalf("expected \"\" for no candidates, got %q", got)
	}
}

func TestFormatCandidatesRendersHeaderEntriesAndHint(t *testing.T) {
	got := FormatCandidates("Detected candidates:", []protocol.CommandCandidate{
		{Name: "test:unit", Run: "npm run test:unit"},
		{Name: "lint", Run: "npm run lint"},
	})
	for _, want := range []string{
		"Detected candidates:",
		"test:unit", "npm run test:unit",
		"lint", "npm run lint",
		"Use declare_command to add one.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Index(got, "test:unit") > strings.Index(got, "lint") {
		t.Fatalf("expected candidates in the order given:\n%s", got)
	}
}

// run_command's empty-name listing shows detected candidates after the
// declared list, so an agent that has never declared anything still sees
// the repository's real vocabulary instead of an empty answer.
func TestRunCommandListingShowsCandidatesAfterDeclared(t *testing.T) {
	out := runCommand(protocol.RunCommandResponse{
		Status:     "listed",
		Summary:    "1 declared commands",
		Available:  []protocol.DeclaredCommand{{Name: "test-integration", Run: "npm run test:integration"}},
		Candidates: []protocol.CommandCandidate{{Name: "lint", Run: "npm run lint"}},
	})
	if !strings.Contains(out, "test-integration") || !strings.Contains(out, "Detected candidates:") || !strings.Contains(out, "lint") {
		t.Fatalf("expected declared and detected sections both present:\n%s", out)
	}
	if strings.Index(out, "test-integration") > strings.Index(out, "Detected candidates:") {
		t.Fatalf("expected declared commands before detected candidates:\n%s", out)
	}
}

// With nothing declared and nothing detected, the listing stays exactly the
// plain "no commands declared" it always was.
func TestRunCommandListingWithoutCandidatesUnchanged(t *testing.T) {
	out := runCommand(protocol.RunCommandResponse{Status: "listed", Summary: "no commands declared — declare one to create .arno/commands.json"})
	if strings.Contains(out, "Detected candidates:") {
		t.Fatalf("expected no candidates section, got:\n%s", out)
	}
}

func TestCapabilitiesShowsDetectedUndeclaredCommands(t *testing.T) {
	out := capabilities(protocol.CapabilitiesResponse{
		Commands:          []string{"test-integration"},
		CommandCandidates: []protocol.CommandCandidate{{Name: "lint", Run: "npm run lint"}},
	})
	if !strings.Contains(out, "declared commands: test-integration") || !strings.Contains(out, "detected, undeclared commands:") || !strings.Contains(out, "lint") {
		t.Fatalf("expected both declared and detected sections:\n%s", out)
	}
}

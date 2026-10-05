package render

import (
	"strings"
	"testing"

	"github.com/julianbei/arno/internal/protocol"
)

// Issue #5 was filed believing a failed check had rolled the edit back. It
// never did: rollback covers an edit that failed to apply, not one that
// applied and then failed validation. The response has to say so, or the
// caller walks away from a dirty tree thinking it is clean.
func TestApplySaysTheEditSurvivedAFailedCheck(t *testing.T) {
	out := apply(protocol.ApplyResponse{
		Applied:      1,
		Changed:      []string{"a.ts"},
		OldRevision:  "r1",
		NewRevision:  "r2",
		CheckOutcome: protocol.OutcomeFailed,
		CheckSummary: "tests failed: 1 of 12",
	})

	if !strings.Contains(out, "not undone") {
		t.Errorf("a failed check must say the edit is still on disk, got:\n%s", out)
	}
	if !strings.Contains(out, "r2") {
		t.Errorf("the response should name the revision to revert from, got:\n%s", out)
	}
}

// A check that passed says nothing extra: the note exists for the case the
// caller would otherwise misread.
func TestApplySaysNothingExtraWhenTheCheckPassed(t *testing.T) {
	out := apply(protocol.ApplyResponse{
		Applied:      1,
		Changed:      []string{"a.ts"},
		OldRevision:  "r1",
		NewRevision:  "r2",
		CheckOutcome: protocol.OutcomePassed,
		CheckSummary: "tests passed: 12",
	})

	if strings.Contains(out, "not undone") {
		t.Errorf("a passing check should not carry the rollback note, got:\n%s", out)
	}
}

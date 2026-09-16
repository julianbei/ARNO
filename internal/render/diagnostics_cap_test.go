package render

import (
	"fmt"
	"strings"
	"testing"

	"github.com/julianbei/arno/internal/protocol"
)

// Past the cap, an edit response lists the diagnostics nearest the edited
// lines and counts the rest, so a file that already carried hundreds does
// not put all of them into every response.
func TestEditResponseCapsDiagnosticsNearestTheEdit(t *testing.T) {
	var diagnostics []protocol.Diagnostic
	for line := 1; line <= 40; line++ {
		level := protocol.DiagnosticError
		if line%2 == 0 {
			level = protocol.DiagnosticWarning
		}
		diagnostics = append(diagnostics, protocol.Diagnostic{Level: level, Path: "a.py", Line: line, Message: fmt.Sprintf("finding %d", line)})
	}
	response := protocol.EditResponse{
		OldRevision: "r1", NewRevision: "r2", Changed: []string{"a.py"},
		Diagnostics: diagnostics,
		Snippets:    []protocol.Snippet{{Path: "a.py", StartLine: 20, EndLine: 22, Source: "x = 1"}},
	}
	text, ok := Text(response)
	if !ok {
		t.Fatal("edit response not rendered")
	}
	shown := 0
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "error ") || strings.HasPrefix(line, "warning ") {
			shown++
		}
	}
	if shown != maxEditDiagnostics {
		t.Fatalf("expected %d diagnostics shown, got %d:\n%s", maxEditDiagnostics, shown, text)
	}
	for _, want := range []string{"a.py:20 finding 20", "a.py:21 finding 21", "a.py:22 finding 22", "a.py:19 finding 19", "a.py:23 finding 23",
		"28 more diagnostics not shown (the 12 nearest the edit are listed)"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "finding 1\n") || strings.Contains(text, "finding 40") {
		t.Errorf("expected the far findings left out:\n%s", text)
	}
	// Nearest first, and at equal distance the error before the warning:
	// 21 is inside the region, 19 and 23 are two lines away.
	if strings.Index(text, "finding 21") > strings.Index(text, "finding 19") || strings.Index(text, "finding 19") > strings.Index(text, "finding 23") {
		t.Errorf("expected findings ordered by distance then level:\n%s", text)
	}
}

// Under the cap nothing changes: every diagnostic, in the order given.
func TestEditResponseUnderTheCapListsEverything(t *testing.T) {
	response := protocol.EditResponse{
		OldRevision: "r1", NewRevision: "r2", Changed: []string{"a.py"},
		Diagnostics: []protocol.Diagnostic{
			{Level: protocol.DiagnosticWarning, Path: "a.py", Line: 9, Message: "second"},
			{Level: protocol.DiagnosticError, Path: "a.py", Line: 3, Message: "first"},
		},
		Checks: protocol.CheckReport{Checked: []string{"fake"}, Preexisting: 260},
	}
	text, _ := Text(response)
	if !strings.Contains(text, "warning a.py:9 second\nerror a.py:3 first") || strings.Contains(text, "more diagnostics") {
		t.Fatalf("expected both diagnostics as given and no cap line:\n%s", text)
	}
	if !strings.Contains(text, "260 pre-existing diagnostics not shown: already there before this edit") {
		t.Fatalf("expected the pre-existing count line:\n%s", text)
	}
}

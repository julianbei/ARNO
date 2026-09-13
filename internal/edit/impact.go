package edit

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/julianbei/jade/internal/jobs"
	"github.com/julianbei/jade/internal/protocol"
)

// maxImpactDeclarations bounds how many touched declarations an impact check
// traces, so an apply that rewrites a whole file does not ask for references
// of every declaration in it.
const maxImpactDeclarations = 20

// impact traces what an apply changed (release plan 0.0.7, "Impact-aware
// validation"): the declarations its edits touched, their callers and the
// test files that reference them. The files returned are the smallest set to
// validate: the edited files and every file referencing a touched
// declaration.
//
// It runs after the edits, so a declaration an edit deleted is not traced.
func (s *Service) impact(edits []protocol.EditOp, changed []string) (protocol.Impact, []string) {
	root := s.workspace.Root()
	touched := map[string]string{} // symbol ID -> path

	for _, op := range edits {
		_, symbols, _, err := s.index.OutlineStructured(op.Path)
		if err != nil {
			continue
		}
		var regions []protocol.Snippet
		if op.NewText != "" {
			regions = snippets(root, op.Path, op.NewText)
		}
		for _, symbol := range symbols {
			if (op.SymbolID != "" && symbol.ID == op.SymbolID) || (op.SymbolName != "" && symbol.Name == op.SymbolName) {
				touched[symbol.ID] = op.Path
				continue
			}
			for _, region := range regions {
				if symbol.From <= region.EndLine && symbol.To >= region.StartLine {
					touched[symbol.ID] = op.Path
					break
				}
			}
		}
	}

	ids := make([]string, 0, len(touched))
	for id := range touched {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) > maxImpactDeclarations {
		ids = ids[:maxImpactDeclarations]
	}

	files := map[string]bool{}
	for _, path := range changed {
		files[path] = true
	}
	callers := map[string]bool{}
	callerFiles := map[string]bool{}
	tests := map[string]bool{}
	for _, id := range ids {
		references, err := s.index.References(touched[id], id)
		if err != nil {
			continue
		}
		for _, reference := range references.References {
			files[reference.Path] = true
			if isTestFile(reference.Path) {
				tests[reference.Path] = true
				continue
			}
			callerFiles[reference.Path] = true
			callers[fmt.Sprintf("%s:%d:%s", reference.Path, reference.Line, reference.Symbol)] = true
		}
	}

	return protocol.Impact{
		Declarations: len(touched),
		Callers:      len(callers),
		CallerFiles:  len(callerFiles),
		Tests:        sortedPaths(tests),
	}, sortedPaths(files)
}

// runImpactTests runs the scoped tests for the files an impact check found:
// each Go package among them, and the test files elsewhere.
func (s *Service) runImpactTests(files []string) (protocol.ValidationOutcome, string) {
	jobID := s.jobs.Start("tests")
	s.jobs.RunScopedTests(jobID, s.workspace.Root(), jobs.TestScope{Kind: "changed"}, files)

	output, finished := s.jobs.Wait(jobID, applyCheckTimeout)
	if !finished {
		return protocol.OutcomeTimedOut, fmt.Sprintf("impact tests did not finish within %s — poll job_status %s", applyCheckTimeout, jobID)
	}
	outcome := jobs.Outcome(output, true)
	if outcome == protocol.OutcomePassed {
		return outcome, ""
	}
	return outcome, output.Summary
}

// impactLine renders an impact report for the apply summary.
func impactLine(impact protocol.Impact) string {
	return fmt.Sprintf("impact: %d declarations · %d callers in %d files · %d likely tests",
		impact.Declarations, impact.Callers, impact.CallerFiles, len(impact.Tests))
}

// isTestFile recognises test files by the naming conventions of the
// ecosystems Jade runs tests for.
func isTestFile(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	slashed := "/" + filepath.ToSlash(path)
	return strings.HasSuffix(base, "_test.go") || strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") ||
		strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py") ||
		strings.Contains(slashed, "/test/") || strings.Contains(slashed, "/tests/") || strings.Contains(slashed, "/__tests__/")
}

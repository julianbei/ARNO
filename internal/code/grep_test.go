package code

import (
	"strings"
	"testing"

	"github.com/julianbei/jade/internal/protocol"
)

func grepFixture(t *testing.T) *Index {
	t.Helper()
	dir := t.TempDir()
	writeGoFile(t, dir, "a.go", `package fixture

type Response struct {
	ChangedPaths []string
	Files        []string
}

func Read(r Response) int {
	return len(r.ChangedPaths)
}
`)
	writeGoFile(t, dir, "b.go", `package fixture

func Other(r Response) int {
	return len(r.ChangedPaths)
}
`)
	writeGoFile(t, dir, "testdata_helper.go", `package fixture

// ChangedPaths is mentioned here too.
`)
	return NewIndex(dir, nil)
}

func TestGrepFindsAStructFieldSearchCannotReach(t *testing.T) {
	// The question every field removal starts with, and the one Search
	// structurally cannot answer: a struct field is not a symbol with a
	// resolvable position.
	response, err := grepFixture(t).Grep(protocol.GrepRequest{Query: "ChangedPaths"})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if response.Total != 4 {
		t.Fatalf("expected every occurrence found, got %d: %+v", response.Total, response.Matches)
	}
	if response.Files != 3 {
		t.Fatalf("expected 3 files, got %d", response.Files)
	}
}

func TestGrepReturnsOnlyLinesThatActuallyContainTheQuery(t *testing.T) {
	// The defect this tool exists to fix: Search answers a name-similarity
	// question and returns declarations that do not contain the query at all.
	response, err := grepFixture(t).Grep(protocol.GrepRequest{Query: "ChangedPaths"})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	for _, match := range response.Matches {
		if !strings.Contains(match.Text, "ChangedPaths") {
			t.Fatalf("returned a line that does not contain the query: %+v", match)
		}
	}
}

func TestGrepReportsPathAndLine(t *testing.T) {
	response, err := grepFixture(t).Grep(protocol.GrepRequest{Query: "func Other"})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if len(response.Matches) != 1 {
		t.Fatalf("expected 1 match, got %+v", response.Matches)
	}
	if response.Matches[0].Path != "b.go" || response.Matches[0].Line != 3 {
		t.Fatalf("expected b.go:3, got %+v", response.Matches[0])
	}
}

func TestGrepReturnsTrailingContextLikeDashA(t *testing.T) {
	// The fused search-and-read the bash form had: `grep -n X -A 3`.
	response, err := grepFixture(t).Grep(protocol.GrepRequest{Query: "func Read", Context: 2})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if len(response.Matches) != 1 {
		t.Fatalf("expected 1 match, got %+v", response.Matches)
	}
	if len(response.Matches[0].After) != 2 {
		t.Fatalf("expected 2 context lines, got %+v", response.Matches[0].After)
	}
	if !strings.Contains(response.Matches[0].After[0], "return len") {
		t.Fatalf("expected the body line as context, got %+v", response.Matches[0].After)
	}
}

func TestGrepContextStopsAtEndOfFile(t *testing.T) {
	response, err := grepFixture(t).Grep(protocol.GrepRequest{Query: "mentioned here", Context: 30})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if len(response.Matches) != 1 {
		t.Fatalf("expected 1 match, got %+v", response.Matches)
	}
	// Asking for more context than the file has must not panic or pad.
	for _, line := range response.Matches[0].After {
		if strings.Contains(line, "\x00") {
			t.Fatalf("unexpected padding in context: %q", line)
		}
	}
}

func TestGrepExcludeDropsMatchingPaths(t *testing.T) {
	// The `| grep -v testdata` half of the command this replaces.
	response, err := grepFixture(t).Grep(protocol.GrepRequest{Query: "ChangedPaths", Exclude: "testdata"})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	for _, match := range response.Matches {
		if strings.Contains(match.Path, "testdata") {
			t.Fatalf("expected excluded path dropped, got %+v", match)
		}
	}
	if response.Total != 3 {
		t.Fatalf("expected 3 matches after exclusion, got %d", response.Total)
	}
}

func TestGrepGlobMatchesBothBaseNameAndPath(t *testing.T) {
	// A caller should not have to know which spelling this implementation
	// prefers.
	index := grepFixture(t)
	for _, glob := range []string{"b.go", "*.go"} {
		response, err := index.Grep(protocol.GrepRequest{Query: "ChangedPaths", Glob: glob})
		if err != nil {
			t.Fatalf("Grep(%q): %v", glob, err)
		}
		if response.Total == 0 {
			t.Fatalf("glob %q matched nothing", glob)
		}
	}

	response, err := index.Grep(protocol.GrepRequest{Query: "ChangedPaths", Glob: "*.rs"})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if response.Total != 0 {
		t.Fatalf("expected a non-matching glob to return nothing, got %+v", response.Matches)
	}
}

func TestGrepIsCaseSensitiveByDefault(t *testing.T) {
	index := grepFixture(t)

	sensitive, err := index.Grep(protocol.GrepRequest{Query: "changedpaths"})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if sensitive.Total != 0 {
		t.Fatalf("expected a case-sensitive miss, got %d", sensitive.Total)
	}

	insensitive, err := index.Grep(protocol.GrepRequest{Query: "changedpaths", IgnoreCase: true})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if insensitive.Total == 0 {
		t.Fatalf("expected ignoreCase to match")
	}
}

func TestGrepRegexMode(t *testing.T) {
	response, err := grepFixture(t).Grep(protocol.GrepRequest{Query: `func (Read|Other)\(`, Regex: true})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if response.Total != 2 {
		t.Fatalf("expected both funcs, got %+v", response.Matches)
	}
}

func TestGrepRejectsABadRegexInsteadOfMatchingLiterally(t *testing.T) {
	// Silently matching something other than what was asked for is the failure
	// mode that makes a search tool untrustworthy.
	_, err := grepFixture(t).Grep(protocol.GrepRequest{Query: "func [", Regex: true})
	if err == nil {
		t.Fatalf("expected an invalid regex to be rejected")
	}
	if !strings.Contains(err.Error(), "regex") {
		t.Fatalf("expected the error to name the problem, got %v", err)
	}

	// An unbalanced parenthesis is read as text, and the answer says so.
	response, err := grepFixture(t).Grep(protocol.GrepRequest{Query: "func (", Regex: true})
	if err != nil {
		t.Fatalf("an unbalanced parenthesis should be searched as text: %v", err)
	}
	if !strings.Contains(response.Summary, "unbalanced parenthesis") {
		t.Fatalf("expected the reading to be stated, got %q", response.Summary)
	}
}

func TestGrepReportsTrueTotalWhenCapped(t *testing.T) {
	// A cap must cost detail, never accuracy.
	response, err := grepFixture(t).Grep(protocol.GrepRequest{Query: "ChangedPaths", Limit: 1})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if len(response.Matches) != 1 {
		t.Fatalf("expected the limit honoured, got %d", len(response.Matches))
	}
	if response.Total != 4 {
		t.Fatalf("expected the true total reported, got %d", response.Total)
	}
	if !response.Truncated {
		t.Fatalf("expected truncation reported")
	}
	if !strings.Contains(response.Summary, "showing") {
		t.Fatalf("expected the summary to say the result was cut, got %q", response.Summary)
	}
}

func TestGrepReportsNoMatchesClearly(t *testing.T) {
	response, err := grepFixture(t).Grep(protocol.GrepRequest{Query: "NothingLikeThisExists"})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if response.Total != 0 || len(response.Matches) != 0 {
		t.Fatalf("expected no matches, got %+v", response)
	}
	if !strings.Contains(response.Summary, "no matches") {
		t.Fatalf("expected a clear summary, got %q", response.Summary)
	}
}

func TestGrepRejectsEmptyQuery(t *testing.T) {
	if _, err := grepFixture(t).Grep(protocol.GrepRequest{Query: "   "}); err == nil {
		t.Fatalf("expected an empty query to be rejected")
	}
}

func TestGrepIsStableAcrossCalls(t *testing.T) {
	// Unstable ordering makes a response undiffable and a re-read look like a
	// change.
	index := grepFixture(t)
	first, _ := index.Grep(protocol.GrepRequest{Query: "ChangedPaths"})
	second, _ := index.Grep(protocol.GrepRequest{Query: "ChangedPaths"})

	if len(first.Matches) != len(second.Matches) {
		t.Fatalf("match count changed between calls")
	}
	for i := range first.Matches {
		if first.Matches[i].Path != second.Matches[i].Path || first.Matches[i].Line != second.Matches[i].Line {
			t.Fatalf("ordering changed at %d", i)
		}
	}
}

func TestGrepClipsPathologicallyLongLines(t *testing.T) {
	// One minified bundle must not dominate the whole response.
	dir := t.TempDir()
	writeGoFile(t, dir, "long.go", "package fixture\n\n// "+strings.Repeat("x", 5000)+" needle\n")

	response, err := NewIndex(dir, nil).Grep(protocol.GrepRequest{Query: "xxx"})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if len(response.Matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(response.Matches))
	}
	if len(response.Matches[0].Text) > maxGrepLineLength+10 {
		t.Fatalf("expected the line clipped, got %d chars", len(response.Matches[0].Text))
	}
}

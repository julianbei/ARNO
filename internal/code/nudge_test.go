package code

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocateQueryExtractsBareArgument(t *testing.T) {
	query, ok := locateQuery("grep -r RefreshSession .")
	if !ok {
		t.Fatalf("expected a query to be located")
	}
	if query != "RefreshSession" {
		t.Fatalf("expected RefreshSession, got %q", query)
	}
}

func TestLocateQueryPrefersQuotedString(t *testing.T) {
	query, ok := locateQuery(`rg "session token"`)
	if !ok {
		t.Fatalf("expected a query to be located")
	}
	if query != "session token" {
		t.Fatalf("expected quoted string extracted, got %q", query)
	}
}

func TestLocateQueryExtractsFlagValue(t *testing.T) {
	query, ok := locateQuery(`find . -name "*.session.go"`)
	if !ok {
		t.Fatalf("expected a query to be located")
	}
	if query != "*.session.go" {
		t.Fatalf("expected the -name flag's value, got %q", query)
	}
}

func TestLocateQueryFindsSegmentAfterPipe(t *testing.T) {
	query, ok := locateQuery("cd src && grep -r RefreshSession . | wc -l")
	if !ok {
		t.Fatalf("expected a query to be located across a chained command")
	}
	if query != "RefreshSession" {
		t.Fatalf("expected RefreshSession, got %q", query)
	}
}

func TestLocateQueryReturnsFalseForNonLocatorCommand(t *testing.T) {
	if _, ok := locateQuery("go build ./..."); ok {
		t.Fatalf("expected no query located for a non-locator command")
	}
}

func TestCleanNudgeQueryRejectsMostlyRegexMetacharacters(t *testing.T) {
	if _, ok := cleanNudgeQuery(`^\|\?[0-9]+$`); ok {
		t.Fatalf("expected a mostly-metacharacter pattern to be rejected")
	}
}

func TestCleanNudgeQueryRejectsTooShort(t *testing.T) {
	if _, ok := cleanNudgeQuery("ab"); ok {
		t.Fatalf("expected a too-short pattern to be rejected")
	}
}

func TestCleanNudgeQueryAcceptsMeaningfulWord(t *testing.T) {
	cleaned, ok := cleanNudgeQuery("RefreshSession")
	if !ok {
		t.Fatalf("expected a meaningful word to be accepted")
	}
	if cleaned != "RefreshSession" {
		t.Fatalf("expected unchanged query, got %q", cleaned)
	}
}

func TestSearchNudgeSkipsSmallOutputThatIsNotFirstInSession(t *testing.T) {
	root := t.TempDir()
	idx := NewIndex(root, nil)

	_, ok := idx.SearchNudge("grep -r RefreshSession .", 50, false)
	if ok {
		t.Fatalf("expected no nudge for small output that isn't the first search")
	}
}

func TestSearchNudgeFiresOnFirstSearchRegardlessOfOutputSize(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "session.go"), []byte("package auth\n\nfunc RefreshSession() string { return \"ok\" }\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	idx := NewIndex(root, nil)
	footer, ok := idx.SearchNudge("grep -r RefreshSession .", 50, true)
	if !ok {
		t.Fatalf("expected a nudge on the first search of the session even with small output")
	}
	if !strings.Contains(footer, "session.go") {
		t.Fatalf("expected footer to reference the matching file, got %q", footer)
	}
}

func TestSearchNudgeFiresOnLargeOutput(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "session.go"), []byte("package auth\n\nfunc RefreshSession() string { return \"ok\" }\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	idx := NewIndex(root, nil)
	footer, ok := idx.SearchNudge("grep -r RefreshSession .", 5000, false)
	if !ok {
		t.Fatalf("expected a nudge for large output")
	}
	if !strings.Contains(footer, "session.go") {
		t.Fatalf("expected footer to reference the matching file, got %q", footer)
	}
}

func TestSearchNudgeNeverReplacesJustAppends(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "session.go"), []byte("package auth\n\nfunc RefreshSession() string { return \"ok\" }\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	idx := NewIndex(root, nil)
	footer, ok := idx.SearchNudge("grep -r RefreshSession .", 5000, false)
	if !ok {
		t.Fatalf("expected a nudge")
	}
	// The contract is additive: SearchNudge only ever returns a footer to
	// append, never anything resembling a directive to drop/replace output.
	if strings.Contains(strings.ToLower(footer), "instead") || strings.Contains(strings.ToLower(footer), "replace") {
		t.Fatalf("expected a purely additive footer, got %q", footer)
	}
}

func TestSearchNudgeReturnsFalseWhenNoHitsMatch(t *testing.T) {
	root := t.TempDir()
	idx := NewIndex(root, nil)

	_, ok := idx.SearchNudge("grep -r NoSuchSymbolAnywhere .", 5000, false)
	if ok {
		t.Fatalf("expected no nudge when the query matches nothing in the index")
	}
}

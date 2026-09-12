package workspace

import (
	"strings"
	"testing"
)

func TestSymbolHistoryReturnsOnlyCommitsTouchingTheRange(t *testing.T) {
	dir := newGitRepo(t)

	// Two functions in one file, changed in separate commits. The whole
	// point of 9.3 is that asking about one must not surface the other's
	// commit, which `git log -p <file>` would.
	writeAt(t, dir, "main.go", "package main\n\nfunc Alpha() int {\n\treturn 1\n}\n\nfunc Beta() int {\n\treturn 1\n}\n")
	gitCommitAll(t, dir, "initial both functions")

	writeAt(t, dir, "main.go", "package main\n\nfunc Alpha() int {\n\treturn 99\n}\n\nfunc Beta() int {\n\treturn 1\n}\n")
	gitCommitAll(t, dir, "change Alpha only")

	writeAt(t, dir, "main.go", "package main\n\nfunc Alpha() int {\n\treturn 99\n}\n\nfunc Beta() int {\n\treturn 77\n}\n")
	gitCommitAll(t, dir, "change Beta only")

	// Lines 7-9 are Beta.
	response, err := NewManager(dir, nil).SymbolHistory("main.go", 7, 9, 10, false)
	if err != nil {
		t.Fatalf("SymbolHistory: %v", err)
	}

	subjects := make([]string, 0, len(response.Commits))
	for _, commit := range response.Commits {
		subjects = append(subjects, commit.Subject)
	}
	joined := strings.Join(subjects, " | ")

	if !strings.Contains(joined, "change Beta only") {
		t.Fatalf("expected Beta's own commit, got %q", joined)
	}
	if strings.Contains(joined, "change Alpha only") {
		t.Fatalf("expected a commit touching only Alpha to be excluded, got %q", joined)
	}
}

func TestSymbolHistoryOmitsPatchByDefault(t *testing.T) {
	dir := newGitRepo(t)
	writeAt(t, dir, "main.go", "package main\n\nfunc Alpha() int {\n\treturn 1\n}\n")
	gitCommitAll(t, dir, "initial")

	response, err := NewManager(dir, nil).SymbolHistory("main.go", 3, 5, 10, false)
	if err != nil {
		t.Fatalf("SymbolHistory: %v", err)
	}
	if response.Patch != "" {
		t.Fatalf("expected no patch by default, got:\n%s", response.Patch)
	}
	if len(response.Commits) == 0 {
		t.Fatalf("expected at least one commit")
	}
}

func TestSymbolHistoryIncludesPatchWhenAsked(t *testing.T) {
	dir := newGitRepo(t)
	writeAt(t, dir, "main.go", "package main\n\nfunc Alpha() int {\n\treturn 1\n}\n")
	gitCommitAll(t, dir, "initial")

	response, err := NewManager(dir, nil).SymbolHistory("main.go", 3, 5, 10, true)
	if err != nil {
		t.Fatalf("SymbolHistory: %v", err)
	}
	if !strings.Contains(response.Patch, "func Alpha()") {
		t.Fatalf("expected hunk content in the patch, got:\n%s", response.Patch)
	}
	// Commit metadata must still parse cleanly out of the patch stream.
	if len(response.Commits) != 1 {
		t.Fatalf("expected 1 commit alongside the patch, got %d", len(response.Commits))
	}
	if response.Commits[0].Subject != "initial" {
		t.Fatalf("expected the subject parsed out of the patch stream, got %+v", response.Commits[0])
	}
}

func TestSymbolHistoryRespectsLimit(t *testing.T) {
	dir := newGitRepo(t)
	writeAt(t, dir, "main.go", "package main\n\nfunc Alpha() int {\n\treturn 0\n}\n")
	gitCommitAll(t, dir, "commit 0")
	for i := 1; i <= 4; i++ {
		writeAt(t, dir, "main.go", "package main\n\nfunc Alpha() int {\n\treturn "+string(rune('0'+i))+"\n}\n")
		gitCommitAll(t, dir, "commit "+string(rune('0'+i)))
	}

	response, err := NewManager(dir, nil).SymbolHistory("main.go", 3, 5, 2, false)
	if err != nil {
		t.Fatalf("SymbolHistory: %v", err)
	}
	if len(response.Commits) != 2 {
		t.Fatalf("expected the limit honoured, got %d commits", len(response.Commits))
	}
}

func TestSymbolHistoryRejectsInvalidRanges(t *testing.T) {
	dir := newGitRepo(t)
	writeAt(t, dir, "main.go", "package main\n")
	gitCommitAll(t, dir, "initial")

	manager := NewManager(dir, nil)
	for _, bad := range [][2]int{{0, 5}, {5, 2}, {-1, 3}} {
		if _, err := manager.SymbolHistory("main.go", bad[0], bad[1], 10, false); err == nil {
			t.Fatalf("expected an error for range %v", bad)
		}
	}
}

func TestSymbolHistoryErrorsForUntrackedFile(t *testing.T) {
	dir := newGitRepo(t)
	writeAt(t, dir, "tracked.go", "package main\n")
	gitCommitAll(t, dir, "initial")
	writeAt(t, dir, "untracked.go", "package main\n\nfunc New() {}\n")

	// A file git has never seen has no history. Reporting an error beats
	// reporting zero commits, which reads as "this code was never touched".
	if _, err := NewManager(dir, nil).SymbolHistory("untracked.go", 3, 3, 10, false); err == nil {
		t.Fatalf("expected an error for a file with no git history")
	}
}

func TestParseHistorySeparatesCommitsFromDiffLines(t *testing.T) {
	// A diff line that resembles a header must not be parsed as one; the NUL
	// separators are what identify a real header.
	output := "abc1234567890" + historyFieldSeparator + "Ada" + historyFieldSeparator + "2026-09-12" + historyFieldSeparator + "Subject here\n" +
		"diff --git a/main.go b/main.go\n" +
		"+some added line\n"

	commits, patch := parseHistory(output, true)
	if len(commits) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(commits))
	}
	if commits[0].SHA != "abc1234" {
		t.Fatalf("expected the SHA abbreviated to 7 chars, got %q", commits[0].SHA)
	}
	if commits[0].Subject != "Subject here" {
		t.Fatalf("unexpected subject %q", commits[0].Subject)
	}
	if !strings.Contains(patch, "+some added line") {
		t.Fatalf("expected diff lines collected into the patch, got %q", patch)
	}
	if strings.Contains(patch, "Subject here") {
		t.Fatalf("expected the header excluded from the patch, got %q", patch)
	}
}

func TestParseHistoryDropsPatchWhenNotCollecting(t *testing.T) {
	output := "abc1234567890" + historyFieldSeparator + "Ada" + historyFieldSeparator + "2026-09-12" + historyFieldSeparator + "S\n" +
		"diff --git a/main.go b/main.go\n"

	_, patch := parseHistory(output, false)
	if patch != "" {
		t.Fatalf("expected no patch collected, got %q", patch)
	}
}

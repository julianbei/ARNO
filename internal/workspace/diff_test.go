package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func newGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available in this environment")
	}

	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	return dir
}

func gitCommitAll(t *testing.T, dir string, message string) {
	t.Helper()
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", message}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
}

func writeAt(t *testing.T, dir string, name string, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestDiffReturnsRealHunkContentForModifiedFile(t *testing.T) {
	dir := newGitRepo(t)
	writeAt(t, dir, "main.go", "package main\n\nfunc main() {\n\tprintln(\"old\")\n}\n")
	gitCommitAll(t, dir, "initial")
	writeAt(t, dir, "main.go", "package main\n\nfunc main() {\n\tprintln(\"new\")\n}\n")

	response, err := NewManager(dir, nil).Diff("")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	// The whole point of 8.2: actual hunk text, not counts.
	if !strings.Contains(response.Patch, "-\tprintln(\"old\")") {
		t.Fatalf("expected the removed line in the patch, got:\n%s", response.Patch)
	}
	if !strings.Contains(response.Patch, "+\tprintln(\"new\")") {
		t.Fatalf("expected the added line in the patch, got:\n%s", response.Patch)
	}
}

func TestDiffIncludesUntrackedFiles(t *testing.T) {
	dir := newGitRepo(t)
	writeAt(t, dir, "tracked.go", "package main\n")
	gitCommitAll(t, dir, "initial")
	writeAt(t, dir, "brand_new.go", "package main\n\nfunc BrandNew() {}\n")

	response, err := NewManager(dir, nil).Diff("")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	// `git diff HEAD` cannot see untracked files. Reporting an empty patch
	// for a file the agent just created would be a silent lie.
	if !strings.Contains(response.Patch, "brand_new.go") {
		t.Fatalf("expected the untracked file in the patch, got:\n%s", response.Patch)
	}
	if !strings.Contains(response.Patch, "+func BrandNew() {}") {
		t.Fatalf("expected the untracked file's content in the patch, got:\n%s", response.Patch)
	}
}

func TestDiffTargetsASinglePath(t *testing.T) {
	dir := newGitRepo(t)
	writeAt(t, dir, "a.go", "package main\n\nfunc A() {}\n")
	writeAt(t, dir, "b.go", "package main\n\nfunc B() {}\n")
	gitCommitAll(t, dir, "initial")
	writeAt(t, dir, "a.go", "package main\n\nfunc A() { println(1) }\n")
	writeAt(t, dir, "b.go", "package main\n\nfunc B() { println(2) }\n")

	response, err := NewManager(dir, nil).Diff("a.go")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !strings.Contains(response.Patch, "a.go") {
		t.Fatalf("expected a.go in the targeted patch, got:\n%s", response.Patch)
	}
	if strings.Contains(response.Patch, "b.go") {
		t.Fatalf("expected b.go to be excluded from a targeted diff, got:\n%s", response.Patch)
	}
}

func TestDiffTargetsAnUntrackedPath(t *testing.T) {
	dir := newGitRepo(t)
	writeAt(t, dir, "tracked.go", "package main\n")
	gitCommitAll(t, dir, "initial")
	writeAt(t, dir, "fresh.go", "package main\n\nfunc Fresh() {}\n")

	response, err := NewManager(dir, nil).Diff("fresh.go")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !strings.Contains(response.Patch, "+func Fresh() {}") {
		t.Fatalf("expected a targeted untracked diff to show content, got:\n%s", response.Patch)
	}
}

func TestDiffReportsNoChangesForCleanTree(t *testing.T) {
	dir := newGitRepo(t)
	writeAt(t, dir, "main.go", "package main\n")
	gitCommitAll(t, dir, "initial")

	response, err := NewManager(dir, nil).Diff("")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if strings.TrimSpace(response.Patch) != "" {
		t.Fatalf("expected an empty patch for a clean tree, got:\n%s", response.Patch)
	}
	if !strings.Contains(response.Summary, "no changes") {
		t.Fatalf("expected the summary to say so, got %q", response.Summary)
	}
	if response.OmittedBytes != 0 {
		t.Fatalf("expected 0 omitted bytes, got %d", response.OmittedBytes)
	}
}

func TestDiffClampsVeryLargePatches(t *testing.T) {
	dir := newGitRepo(t)
	writeAt(t, dir, "big.go", "package main\n")
	gitCommitAll(t, dir, "initial")

	var builder strings.Builder
	builder.WriteString("package main\n")
	for i := 0; i < 3000; i++ {
		builder.WriteString("// a comment line long enough to add up to real bytes\n")
	}
	writeAt(t, dir, "big.go", builder.String())

	response, err := NewManager(dir, nil).Diff("")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if response.OmittedBytes <= 0 {
		t.Fatalf("expected a large patch to be clamped, got %d omitted", response.OmittedBytes)
	}
	if !strings.Contains(response.Patch, "bytes omitted") {
		t.Fatalf("expected an explicit omission marker in the clamped patch")
	}
	if !strings.Contains(response.Summary, "omitted") {
		t.Fatalf("expected the summary to report the omission, got %q", response.Summary)
	}
}

func TestDiffSummaryCountsFiles(t *testing.T) {
	dir := newGitRepo(t)
	writeAt(t, dir, "a.go", "package main\n")
	writeAt(t, dir, "b.go", "package main\n")
	gitCommitAll(t, dir, "initial")
	writeAt(t, dir, "a.go", "package main\n\nfunc A() {}\n")
	writeAt(t, dir, "b.go", "package main\n\nfunc B() {}\n")

	response, err := NewManager(dir, nil).Diff("")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !strings.Contains(response.Summary, "2 changed file(s)") {
		t.Fatalf("expected 2 files counted, got %q", response.Summary)
	}
}
func commitFile(t *testing.T, root string, name string, content string, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", message)
}

func diffRepo(t *testing.T) (*Manager, string) {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test")
	return NewManager(root, nil), root
}

func TestDiffSinceSeesWorkThatIsAlreadyCommitted(t *testing.T) {
	// The case the working-tree diff structurally cannot answer: a task that
	// commits midway vanishes from `git diff HEAD` while being exactly what
	// the caller wanted to see.
	m, root := diffRepo(t)
	commitFile(t, root, "a.go", "package probe\n", "initial")
	commitFile(t, root, "a.go", "package probe\n\nfunc Added() {}\n", "add a function")

	head, err := m.Diff("")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if strings.Contains(head.Patch, "func Added") {
		t.Fatalf("committed work should not show in the working-tree diff")
	}

	since, err := m.DiffSince("", "HEAD~1")
	if err != nil {
		t.Fatalf("DiffSince: %v", err)
	}
	if !strings.Contains(since.Patch, "func Added") {
		t.Fatalf("expected the committed change, got:\n%s", since.Patch)
	}
}

func TestDiffSinceIncludesUncommittedWorkToo(t *testing.T) {
	// `git diff <rev>` spans the revision through to the working tree, so a
	// branch half-committed and half-in-progress reads as one change.
	m, root := diffRepo(t)
	commitFile(t, root, "a.go", "package probe\n", "initial")
	commitFile(t, root, "a.go", "package probe\n\nfunc Committed() {}\n", "commit one")
	if err := os.WriteFile(filepath.Join(root, "a.go"),
		[]byte("package probe\n\nfunc Committed() {}\n\nfunc Pending() {}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	since, err := m.DiffSince("", "HEAD~1")
	if err != nil {
		t.Fatalf("DiffSince: %v", err)
	}
	if !strings.Contains(since.Patch, "func Committed") || !strings.Contains(since.Patch, "func Pending") {
		t.Fatalf("expected both halves of the branch, got:\n%s", since.Patch)
	}
}

func TestDiffSinceNarrowsToOnePath(t *testing.T) {
	m, root := diffRepo(t)
	commitFile(t, root, "a.go", "package probe\n", "initial")
	runGit(t, root, "rm", "--cached", "-q", "a.go")
	commitFile(t, root, "a.go", "package probe\n\nfunc InA() {}\n", "change a")
	commitFile(t, root, "b.go", "package probe\n\nfunc InB() {}\n", "add b")

	since, err := m.DiffSince("a.go", "HEAD~2")
	if err != nil {
		t.Fatalf("DiffSince: %v", err)
	}
	if strings.Contains(since.Patch, "func InB") {
		t.Fatalf("expected the path filter honoured, got:\n%s", since.Patch)
	}
}

func TestDiffSinceReportsABadRevisionWithTheSpellingUsed(t *testing.T) {
	// The usual cause is a typo or a revision that does not exist here, so the
	// error names what was asked for rather than relaying raw git noise.
	m, root := diffRepo(t)
	commitFile(t, root, "a.go", "package probe\n", "initial")

	_, err := m.DiffSince("", "no-such-branch")
	if err == nil {
		t.Fatalf("expected an unknown revision to be reported")
	}
	if !strings.Contains(err.Error(), "no-such-branch") {
		t.Fatalf("expected the error to quote the revision, got %v", err)
	}
}

func TestDiffSinceEmptyIsExactlyDiff(t *testing.T) {
	// The old entry point must keep behaving identically, untracked files and
	// all.
	m, root := diffRepo(t)
	commitFile(t, root, "a.go", "package probe\n", "initial")
	if err := os.WriteFile(filepath.Join(root, "new.go"), []byte("package probe\n\nfunc Fresh() {}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	plain, err := m.Diff("")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	viaSince, err := m.DiffSince("", "")
	if err != nil {
		t.Fatalf("DiffSince: %v", err)
	}
	if plain.Patch != viaSince.Patch {
		t.Fatalf("expected identical patches")
	}
	if !strings.Contains(plain.Patch, "func Fresh") {
		t.Fatalf("expected untracked files still included, got:\n%s", plain.Patch)
	}
}

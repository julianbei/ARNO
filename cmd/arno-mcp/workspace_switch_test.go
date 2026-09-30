package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// twoWorktrees builds a repository with a second worktree and returns both
// paths. Skips rather than fails where git is missing: the rest of the suite
// runs on machines without one.
func twoWorktrees(t *testing.T) (main string, side string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}

	base := t.TempDir()
	main = filepath.Join(base, "main")
	side = filepath.Join(base, "side")

	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatal(err)
	}
	run(main, "init", "-q", ".")
	if err := os.WriteFile(filepath.Join(main, "file.txt"), []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(main, "add", "-A")
	run(main, "commit", "-qm", "init")
	run(main, "worktree", "add", "-q", side, "-b", "sidebranch")
	return main, side
}

// A repository's worktrees are discoverable from any of them, which is what
// makes switching back the same operation as switching away.
func TestGitWorktreesListsEveryWorktreeFromEither(t *testing.T) {
	main, side := twoWorktrees(t)

	for _, from := range []string{main, side} {
		list, err := gitWorktrees(context.Background(), from)
		if err != nil {
			t.Fatalf("from %s: %v", from, err)
		}
		if len(list) != 2 {
			t.Fatalf("from %s: expected 2 worktrees, got %d: %+v", from, len(list), list)
		}

		var current string
		for _, w := range list {
			if w.current {
				current = w.path
			}
		}
		if !sameWorkspaceDir(current, from) {
			t.Errorf("from %s: the current worktree is marked %s", from, current)
		}
	}
}

// A directory that is not a git repository can list nothing, so switching is
// unavailable there rather than unrestricted.
func TestGitWorktreesRefusesANonRepository(t *testing.T) {
	if _, err := gitWorktrees(context.Background(), t.TempDir()); err == nil {
		t.Fatal("expected an error for a directory that is not a git repository")
	}
}

// The containment rule: only this repository's own worktrees. Without it a
// session pointed at one repository could be talked into writing anywhere on
// disk, which is the property pathguard gives within a root.
func TestSwitchRefusesADirectoryOutsideTheRepository(t *testing.T) {
	main, _ := twoWorktrees(t)
	s := &mcpServer{graph: &workspaceGraph{root: main}}

	outside := t.TempDir()
	_, err := s.switchWorkspace(context.Background(), outside)
	if err == nil {
		t.Fatal("expected a refusal for a directory outside the repository")
	}
	if !strings.Contains(err.Error(), "not a worktree of this repository") {
		t.Errorf("the refusal should say why, got: %v", err)
	}
	// The refusal names the valid targets, so the caller can act on it
	// without a second call.
	if !strings.Contains(err.Error(), main) {
		t.Errorf("the refusal should list the worktrees, got: %v", err)
	}
}

// Switching to the workspace already being served changes nothing, rather
// than paying for a rebuild and throwing away the revision history.
func TestSwitchToTheCurrentWorkspaceIsANoop(t *testing.T) {
	main, _ := twoWorktrees(t)
	s := &mcpServer{graph: &workspaceGraph{root: main}}

	text, err := s.switchWorkspace(context.Background(), main)
	if err != nil {
		t.Fatalf("switching to the current workspace: %v", err)
	}
	if !strings.Contains(text, "nothing changed") {
		t.Errorf("expected a no-op, got: %s", text)
	}
}

// With no target the tool reports where writes are going and what else this
// session may serve — the question issue #3 exists because nothing answered.
func TestDescribeNamesTheActiveWorkspaceAndTheAlternatives(t *testing.T) {
	main, side := twoWorktrees(t)
	list, err := gitWorktrees(context.Background(), main)
	if err != nil {
		t.Fatal(err)
	}

	text := describeWorkspaces(main, list)
	// git prints its own resolved spelling of a path — /private/var on macOS
	// where the caller said /var — so the listing is checked by what each
	// line resolves to, not by string equality with the path passed in.
	var marked string
	for _, line := range strings.Split(text, "\n") {
		if rest, ok := strings.CutPrefix(line, "* "); ok {
			marked, _, _ = strings.Cut(rest, " (")
		}
	}
	if !sameWorkspaceDir(marked, main) {
		t.Errorf("the active workspace should be marked, %q is, got:\n%s", marked, text)
	}
	if !strings.Contains(text, filepath.Base(side)) {
		t.Errorf("the other worktree should be listed, got:\n%s", text)
	}
	if !strings.Contains(text, "sidebranch") {
		t.Errorf("a worktree's branch should be named, got:\n%s", text)
	}
}

// A repository with one worktree has nothing to switch to, and says so
// instead of offering a list of one.
func TestDescribeSaysWhenThereIsNowhereToSwitch(t *testing.T) {
	text := describeWorkspaces("/repo", []worktree{{path: "/repo", current: true}})
	if !strings.Contains(text, "nothing to switch to") {
		t.Errorf("expected the single-worktree case to say so, got:\n%s", text)
	}
}

// A path that reaches the same directory by another spelling is the same
// workspace: /tmp and /private/tmp on macOS, or any symlinked checkout.
func TestSameWorkspaceDirResolvesSpellings(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if !sameWorkspaceDir(dir, link) {
		t.Errorf("%s and %s name the same directory", dir, link)
	}
	if sameWorkspaceDir(dir, filepath.Join(dir, "sub")) {
		t.Error("a subdirectory is not the same workspace")
	}
}

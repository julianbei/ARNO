package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func noEnv(string) string { return "" }

func cwdAt(dir string) func() (string, error) {
	return func() (string, error) { return dir, nil }
}

func TestRootFlagWinsOverEnvironment(t *testing.T) {
	// The flag is the more specific statement: an env var is usually inherited
	// from a shell or client config and may not have been written with this
	// invocation in mind.
	flagged := t.TempDir()
	fromEnv := t.TempDir()

	resolved, err := resolveWorkspaceRoot(
		[]string{"--root", flagged},
		func(string) string { return fromEnv },
		cwdAt(t.TempDir()),
	)
	if err != nil {
		t.Fatalf("resolveWorkspaceRoot: %v", err)
	}
	if !sameDir(t, resolved.Path, flagged) {
		t.Fatalf("expected the flag to win, got %s", resolved.Path)
	}
	if resolved.Source != rootFromFlag {
		t.Fatalf("expected the source reported as the flag, got %q", resolved.Source)
	}
}

func TestEnvironmentWinsOverWorkingDirectory(t *testing.T) {
	fromEnv := t.TempDir()
	resolved, err := resolveWorkspaceRoot(nil, func(string) string { return fromEnv }, cwdAt(t.TempDir()))
	if err != nil {
		t.Fatalf("resolveWorkspaceRoot: %v", err)
	}
	if !sameDir(t, resolved.Path, fromEnv) {
		t.Fatalf("expected the env var used, got %s", resolved.Path)
	}
	if resolved.Source != rootFromEnv {
		t.Fatalf("expected the source reported as the env var, got %q", resolved.Source)
	}
}

func TestWorkingDirectoryFallbackIsReportedNotSilent(t *testing.T) {
	// The point of 11.2: falling back to cwd is convenient and fine, but
	// silently operating on whatever directory a client launched from is how
	// an agent confidently edits the wrong repository.
	dir := t.TempDir()
	resolved, err := resolveWorkspaceRoot(nil, noEnv, cwdAt(dir))
	if err != nil {
		t.Fatalf("resolveWorkspaceRoot: %v", err)
	}
	if resolved.Source != rootFromCwd {
		t.Fatalf("expected the fallback to name itself, got %q", resolved.Source)
	}
}

func TestRootFlagAcceptsBothSpellings(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{
		{"--root", dir},
		{"--root=" + dir},
		{"-root", dir},
		{"-root=" + dir},
	} {
		resolved, err := resolveWorkspaceRoot(args, noEnv, cwdAt(t.TempDir()))
		if err != nil {
			t.Fatalf("args %v: %v", args, err)
		}
		if !sameDir(t, resolved.Path, dir) {
			t.Fatalf("args %v: got %s", args, resolved.Path)
		}
	}
}

func TestEmptyRootFlagIsAnErrorNotAFallThrough(t *testing.T) {
	// Someone who passed --root meant to choose a directory. Quietly using a
	// different one would be worse than saying the value is missing.
	for _, args := range [][]string{{"--root="}, {"--root"}, {"--root", "--other"}} {
		if _, err := resolveWorkspaceRoot(args, func(string) string { return t.TempDir() }, cwdAt(t.TempDir())); err == nil {
			t.Fatalf("args %v: expected an error", args)
		}
	}
}

func TestMissingRootIsRejectedWithItsSource(t *testing.T) {
	// The error has to say where the bad value came from, or the operator has
	// to guess which of three places to fix.
	missing := filepath.Join(t.TempDir(), "no-such-dir")

	_, err := resolveWorkspaceRoot([]string{"--root", missing}, noEnv, cwdAt(t.TempDir()))
	if err == nil {
		t.Fatalf("expected a missing root to be rejected")
	}
	if !strings.Contains(err.Error(), string(rootFromFlag)) {
		t.Fatalf("expected the error to name the source, got %v", err)
	}
	if !strings.Contains(err.Error(), "no-such-dir") {
		t.Fatalf("expected the error to name the path, got %v", err)
	}
}

func TestAFileIsNotAWorkspaceRoot(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := resolveWorkspaceRoot([]string{"--root", file}, noEnv, cwdAt(dir))
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("expected a file to be rejected as a root, got %v", err)
	}
}

func TestNonGitRootWarnsButStarts(t *testing.T) {
	// Deliberately not fatal. Outline, find, grep, read and every edit work on
	// any directory, and jade's revision tracking is its own counter rather
	// than git's — refusing to start would make jade unusable somewhere it
	// serves perfectly well. But it must be said, because the git-backed tools
	// degrade in ways that look like "nothing changed" rather than an error.
	dir := t.TempDir()

	resolved, err := resolveWorkspaceRoot([]string{"--root", dir}, noEnv, cwdAt(dir))
	if err != nil {
		t.Fatalf("expected a non-git directory to be usable, got %v", err)
	}
	if resolved.Warning == "" {
		t.Fatalf("expected a warning for a non-git root")
	}
	for _, named := range []string{"changes", "diff", "history"} {
		if !strings.Contains(resolved.Warning, named) {
			t.Fatalf("expected the warning to name what degrades, missing %q: %s", named, resolved.Warning)
		}
	}
}

func TestGitRootProducesNoWarning(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	resolved, err := resolveWorkspaceRoot([]string{"--root", dir}, noEnv, cwdAt(dir))
	if err != nil {
		t.Fatalf("resolveWorkspaceRoot: %v", err)
	}
	if resolved.Warning != "" {
		t.Fatalf("expected no warning for a git repo, got %q", resolved.Warning)
	}
}

func TestWorktreeGitFileCountsAsARepository(t *testing.T) {
	// .git is a directory in a normal clone and a *file* in a worktree or
	// submodule. Treating only the directory as real would warn spuriously in
	// every worktree.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: /elsewhere\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	resolved, err := resolveWorkspaceRoot([]string{"--root", dir}, noEnv, cwdAt(dir))
	if err != nil {
		t.Fatalf("resolveWorkspaceRoot: %v", err)
	}
	if resolved.Warning != "" {
		t.Fatalf("expected a worktree to count as a repository, got %q", resolved.Warning)
	}
}

func TestRootIsAlwaysAbsolute(t *testing.T) {
	// Relative roots would make every path jade reports depend on the client's
	// launch directory.
	resolved, err := resolveWorkspaceRoot([]string{"--root", "."}, noEnv, cwdAt(t.TempDir()))
	if err != nil {
		t.Fatalf("resolveWorkspaceRoot: %v", err)
	}
	if !filepath.IsAbs(resolved.Path) {
		t.Fatalf("expected an absolute path, got %s", resolved.Path)
	}
}

func TestUnreadableWorkingDirectoryIsReportedClearly(t *testing.T) {
	_, err := resolveWorkspaceRoot(nil, noEnv, func() (string, error) {
		return "", errors.New("getwd exploded")
	})
	if err == nil {
		t.Fatalf("expected the failure to surface")
	}
	if !strings.Contains(err.Error(), envWorkspace) {
		t.Fatalf("expected the error to suggest the env var, got %v", err)
	}
}

// sameDir compares two directories after symlink resolution, since macOS
// resolves /tmp to /private/tmp and a literal string comparison would fail on
// paths that are in fact identical.
func sameDir(t *testing.T, got string, want string) bool {
	t.Helper()
	resolvedWant, err := filepath.EvalSymlinks(want)
	if err != nil {
		resolvedWant = want
	}
	return got == resolvedWant
}

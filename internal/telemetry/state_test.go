package telemetry

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestStateDirKeepsTheWorkspaceClean(t *testing.T) {
	root := t.TempDir()
	state := t.TempDir()
	t.Setenv(StateDirEnv, state)

	recorder := New(root)
	recorder.Record("arno.find", 0, 10, nil)

	if _, err := os.Stat(filepath.Join(root, Dir)); !os.IsNotExist(err) {
		t.Fatalf("ARNO_STATE_DIR is set, but .arno/ was created in the workspace: %v", err)
	}
	records, err := recorder.Read()
	if err != nil || len(records) != 1 {
		t.Fatalf("expected the record readable from the state dir, got %v, %v", records, err)
	}
	if !strings.HasPrefix(recorder.DisplayPath(), state) {
		t.Fatalf("display path should name the real location, got %q", recorder.DisplayPath())
	}
}

func TestStateDirSeparatesWorkspaces(t *testing.T) {
	state := t.TempDir()
	t.Setenv(StateDirEnv, state)

	// Same base name, different checkouts: the case a plain base-name key
	// would silently merge.
	first := filepath.Join(t.TempDir(), "app")
	second := filepath.Join(t.TempDir(), "app")
	for _, dir := range []string{first, second} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	New(first).Record("arno.find", 0, 1, nil)
	New(second).Record("arno.grep", 0, 1, nil)
	New(second).Record("arno.grep", 0, 1, nil)

	if records, _ := New(first).Read(); len(records) != 1 {
		t.Fatalf("first workspace sees %d records, want 1", len(records))
	}
	if records, _ := New(second).Read(); len(records) != 2 {
		t.Fatalf("second workspace sees %d records, want 2", len(records))
	}
}

func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	run(t, root, "git", "init", "-q")
	return root
}

func run(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, output)
	}
	return string(output)
}

// The harness failure this exists for: an agent's worktree committed
// wholesale, with ARNO's log in it.
func TestLogInAGitWorkspaceIsNotUntracked(t *testing.T) {
	root := gitRepo(t)
	recorder := New(root)

	recorder.Record("arno.find", 0, 1, nil)
	recorder.Record("arno.find", 0, 1, nil)

	if status := run(t, root, "git", "status", "--porcelain", "--untracked-files=all"); strings.Contains(status, "telemetry") {
		t.Fatalf("the telemetry log shows as untracked:\n%s", status)
	}
	exclude, err := os.ReadFile(filepath.Join(root, ".git", "info", "exclude"))
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(exclude), "/.arno/telemetry.jsonl"); count != 1 {
		t.Fatalf("expected the pattern exactly once, found %d:\n%s", count, exclude)
	}
	if _, err := os.Stat(filepath.Join(root, ".gitignore")); !os.IsNotExist(err) {
		t.Fatal(".gitignore is tracked; ARNO must not create or edit it")
	}
}

func TestWorkspaceBelowTheRepositoryRootIsExcludedAtItsOwnPath(t *testing.T) {
	repo := gitRepo(t)
	root := filepath.Join(repo, "services", "api")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	New(root).Record("arno.find", 0, 1, nil)

	if status := run(t, repo, "git", "status", "--porcelain", "--untracked-files=all"); strings.Contains(status, "telemetry") {
		t.Fatalf("a nested workspace's log shows as untracked:\n%s", status)
	}
}

func TestAlreadyIgnoredLogLeavesExcludeAlone(t *testing.T) {
	root := gitRepo(t)
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".arno/telemetry.jsonl\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	excludePath := filepath.Join(root, ".git", "info", "exclude")
	before, _ := os.ReadFile(excludePath)

	New(root).Record("arno.find", 0, 1, nil)

	after, _ := os.ReadFile(excludePath)
	if string(before) != string(after) {
		t.Fatalf("exclude was edited although .gitignore already covers the log:\n%s", after)
	}
}

func TestNonGitWorkspaceStillRecords(t *testing.T) {
	root := t.TempDir()
	recorder := New(root)
	recorder.Record("arno.find", 0, 1, nil)
	if records, _ := recorder.Read(); len(records) != 1 {
		t.Fatalf("expected recording to work outside git, got %d records", len(records))
	}
}

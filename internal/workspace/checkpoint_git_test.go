package workspace

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func checkpointRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available in this environment")
	}
	root := t.TempDir()
	runGit(t, root, "init", "-q")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test")
	writeAt(t, root, "a.go", "package main\n\nfunc A() int { return 1 }\n")
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-q", "-m", "initial")
	return root
}

func TestCheckpointRecordsTheCommitItWasTakenAt(t *testing.T) {
	root := checkpointRepo(t)
	m := NewManager(root, nil)

	checkpoint := m.Checkpoint("before refactor")
	if checkpoint.Head == "" || len(checkpoint.Head) < 7 {
		t.Fatalf("expected the HEAD commit recorded, got %q", checkpoint.Head)
	}
}

// The case that made agents avoid revert: work committed from the shell after
// the checkpoint would be silently overwritten.
func TestRevertRefusesAcrossACommitAndLeavesFilesAlone(t *testing.T) {
	root := checkpointRepo(t)
	m := NewManager(root, nil)
	m.BumpRevision("a.go")
	checkpoint := m.Checkpoint("")

	committed := "package main\n\nfunc A() int { return 2 }\n"
	writeAt(t, root, "a.go", committed)
	runGit(t, root, "commit", "-q", "-am", "change A")

	_, err := m.RevertCheckpoint(checkpoint.ID)
	if err == nil {
		t.Fatal("expected revert to refuse after a commit landed")
	}
	if !strings.Contains(err.Error(), "committed") || !strings.Contains(err.Error(), shortCommit(checkpoint.Head)) {
		t.Fatalf("expected the refusal to name the commit and the reason, got: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(root, "a.go"))
	if string(data) != committed {
		t.Fatalf("a refused revert must not touch files, a.go is now:\n%s", data)
	}
}

// Within the stretch of work since the last commit, revert is what it was.
func TestRevertWithoutAnInterveningCommitRestoresFiles(t *testing.T) {
	root := checkpointRepo(t)
	m := NewManager(root, nil)
	m.BumpRevision("a.go")
	checkpoint := m.Checkpoint("")

	writeAt(t, root, "a.go", "package main\n\nfunc A() int { return 99 }\n")
	m.BumpRevision("a.go")

	if _, err := m.RevertCheckpoint(checkpoint.ID); err != nil {
		t.Fatalf("revert with no commit in between should succeed: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(root, "a.go"))
	if !strings.Contains(string(data), "return 1") {
		t.Fatalf("expected the checkpoint's content restored, got:\n%s", data)
	}
}

func TestRevertToAnUnknownCheckpointIsNotFound(t *testing.T) {
	m := NewManager(t.TempDir(), nil)
	if _, err := m.RevertCheckpoint("cp-99"); !errors.Is(err, ErrCheckpointNotFound) {
		t.Fatalf("expected ErrCheckpointNotFound, got %v", err)
	}
}

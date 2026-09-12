package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/julianbei/jade/internal/protocol"
)

func TestCheckpointRevertRestoresActualFileBytes(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "greeter.go")
	original := "package main\n\nfunc Greet() string {\n\treturn \"hi\"\n}\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	m := NewManager(root, nil)

	// Simulate a first edit already having happened before the checkpoint.
	m.BumpRevision("greeter.go")

	checkpoint := m.Checkpoint("before second edit")

	// A second edit changes the file on disk after the checkpoint.
	if err := os.WriteFile(path, []byte("package main\n\nfunc Greet() string {\n\treturn \"changed\"\n}\n"), 0o644); err != nil {
		t.Fatalf("write second edit: %v", err)
	}
	m.BumpRevision("greeter.go")

	restored, ok := m.RevertCheckpoint(checkpoint.ID)
	if !ok {
		t.Fatalf("expected revert to succeed")
	}
	if restored.Revision != checkpoint.Revision {
		t.Fatalf("expected revision to be restored to %s, got %s", checkpoint.Revision, restored.Revision)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back file: %v", err)
	}
	if string(data) != original {
		t.Fatalf("expected file bytes to match checkpoint exactly, got %q want %q", string(data), original)
	}
}

func TestCheckpointRevertRestoresBytesForSymbolReplaceChangedKeys(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "greeter.go")
	original := "package main\n\nfunc Greet() string {\n\treturn \"hi\"\n}\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	m := NewManager(root, nil)

	// replace_symbol records the changed-set key as a full symbol ID
	// ("path::Name@line"), not a plain path.
	m.BumpRevision("greeter.go::Greet@3")
	checkpoint := m.Checkpoint("before second edit")

	if err := os.WriteFile(path, []byte("package main\n\nfunc Greet() string {\n\treturn \"changed\"\n}\n"), 0o644); err != nil {
		t.Fatalf("write second edit: %v", err)
	}
	m.BumpRevision("greeter.go::Greet@3")

	if _, ok := m.RevertCheckpoint(checkpoint.ID); !ok {
		t.Fatalf("expected revert to succeed")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back file: %v", err)
	}
	if string(data) != original {
		t.Fatalf("expected file bytes to match checkpoint exactly for a symbol-id changed-key, got %q want %q", string(data), original)
	}
}

func TestChangesIncludesEditsMadeOutsideJade(t *testing.T) {
	root := t.TempDir()
	if err := exec.Command("git", "init", root).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}

	path := filepath.Join(root, "outside.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	m := NewManager(root, nil)

	// No BumpRevision call here at all: this file was never touched through
	// jade, only through a plain os.WriteFile, simulating an edit made by
	// the host's own file tools instead of jade's replace_symbol/replace_range.
	changes := m.Changes()

	found := false
	for _, p := range changes {
		if p == "outside.go" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected Changes() to include a file modified outside jade via real git state, got %#v", changes)
	}
}

func TestChangesNormalizesSymbolIDKeysToPlainPaths(t *testing.T) {
	root := t.TempDir()
	if err := exec.Command("git", "init", root).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}

	m := NewManager(root, nil)
	m.BumpRevision("internal/code/index.go::min@747")

	changes := m.Changes()
	for _, p := range changes {
		if strings.Contains(p, "::") {
			t.Fatalf("expected Changes() to normalize symbol-id keys to plain paths, got raw key %q in %#v", p, changes)
		}
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestDiffSummaryReportsAddedAndRemovedLines(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test")

	path := filepath.Join(root, "greeter.go")
	if err := os.WriteFile(path, []byte("package main\n\nfunc Greet() string {\n\treturn \"hi\"\n}\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "initial")

	if err := os.WriteFile(path, []byte("package main\n\nfunc Greet() string {\n\treturn \"hello there\"\n}\n"), 0o644); err != nil {
		t.Fatalf("modify file: %v", err)
	}

	newPath := filepath.Join(root, "extra.go")
	if err := os.WriteFile(newPath, []byte("package main\n\nfunc Extra() {}\n"), 0o644); err != nil {
		t.Fatalf("write new file: %v", err)
	}

	m := NewManager(root, nil)
	files := m.DiffSummary()

	var greeterFile, extraFile *protocol.ChangedFile
	for i := range files {
		switch files[i].Path {
		case "greeter.go":
			greeterFile = &files[i]
		case "extra.go":
			extraFile = &files[i]
		}
	}

	if greeterFile == nil {
		t.Fatalf("expected greeter.go in diff summary, got %#v", files)
	}
	if greeterFile.Added == 0 || greeterFile.Removed == 0 {
		t.Fatalf("expected greeter.go to show both added and removed lines for a modified line, got %#v", greeterFile)
	}

	if extraFile == nil {
		t.Fatalf("expected untracked extra.go to appear in diff summary, got %#v", files)
	}
	if extraFile.Added == 0 {
		t.Fatalf("expected untracked file to be counted as fully added, got %#v", extraFile)
	}
	if extraFile.Removed != 0 {
		t.Fatalf("expected untracked file to have 0 removed, got %#v", extraFile)
	}
}

func TestChangesResponseSummarizesFileCountAndTotals(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test")

	path := filepath.Join(root, "greeter.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "initial")

	if err := os.WriteFile(path, []byte("package main\n\nfunc Greet() {}\n"), 0o644); err != nil {
		t.Fatalf("modify file: %v", err)
	}

	m := NewManager(root, nil)
	resp := m.ChangesResponse()

	if resp.TotalAdded == 0 {
		t.Fatalf("expected non-zero TotalAdded, got %#v", resp)
	}
	if len(resp.Files) == 0 {
		t.Fatalf("expected at least one file in the summary, got %#v", resp)
	}
	if resp.Summary == "" {
		t.Fatalf("expected a non-empty summary string")
	}
}
func TestChangesResponseListsJadeTouchedFilesGitSeesNoDeltaFor(t *testing.T) {
	// The reason ChangesResponse folds the edit ledger into Files rather than
	// carrying a second Paths list. A file jade wrote and then restored to its
	// committed content has no git delta, but jade still touched it, and the
	// component that knows that must not report a clean tree.
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test")

	path := filepath.Join(root, "touched.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "initial")

	m := NewManager(root, nil)
	m.BumpRevision("touched.go")

	resp := m.ChangesResponse()

	var found bool
	for _, f := range resp.Files {
		if f.Path == "touched.go" {
			found = true
			if f.Added != 0 || f.Removed != 0 {
				t.Fatalf("expected zero counts for a file git sees no delta for, got %#v", f)
			}
		}
	}
	if !found {
		t.Fatalf("expected the touched file listed in Files, got %#v", resp.Files)
	}
}

func TestChangesResponseDoesNotDuplicateAFileGitAlsoReports(t *testing.T) {
	// The merge must be a union, not a concatenation: the common case is a file
	// that is in both the ledger and git's diff, and listing it twice is the
	// exact duplication this change removed.
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test")

	path := filepath.Join(root, "both.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "initial")

	if err := os.WriteFile(path, []byte("package main\n\nfunc Added() {}\n"), 0o644); err != nil {
		t.Fatalf("modify file: %v", err)
	}

	m := NewManager(root, nil)
	m.BumpRevision("both.go")

	count := 0
	for _, f := range m.ChangesResponse().Files {
		if f.Path == "both.go" {
			count++
			if f.Added == 0 {
				t.Fatalf("expected git's real counts to survive the merge, got %#v", f)
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one entry for both.go, got %d", count)
	}
}
func TestChangesResponseOmitsDirectoriesFromTheLedger(t *testing.T) {
	// `git status --porcelain` reports a wholly-untracked directory as a single
	// `dir/` entry, so the changed-path ledger contains directories. They must
	// not reach Files: a `+0 -0 internal/bench` row is not a changed file, and
	// it is exactly what showed up live when the ledger was first folded in.
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test")

	if err := os.WriteFile(filepath.Join(root, "seed.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "initial")

	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg", "real.go"), []byte("package pkg\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	m := NewManager(root, nil)
	m.BumpRevision("pkg", "pkg/real.go")

	for _, f := range m.ChangesResponse().Files {
		if f.Path == "pkg" {
			t.Fatalf("expected the directory omitted, got %#v", f)
		}
	}
}

func TestChangesResponseKeepsALedgerPathThatNoLongerExists(t *testing.T) {
	// A stat failure must not be read as "directory". A file jade edited and
	// that was then deleted is still a file it changed.
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test")

	if err := os.WriteFile(filepath.Join(root, "seed.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "initial")

	m := NewManager(root, nil)
	m.BumpRevision("vanished.go")

	var found bool
	for _, f := range m.ChangesResponse().Files {
		if f.Path == "vanished.go" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a recorded-but-absent path to survive the merge")
	}
}
func TestHeadCommitOnAFreshRepoSaysSoInOneLine(t *testing.T) {
	// Freshness puts this error into every inspect and outline response, so on
	// a brand-new repository jade was emitting four lines of git's "ambiguous
	// argument 'HEAD'" diagnostic at the top of every response — in the
	// operator's locale, which happened to be German. Found live.
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test")

	_, err := NewManager(root, nil).HeadCommit()
	if err != ErrNoCommits {
		t.Fatalf("expected ErrNoCommits, got %v", err)
	}
	if strings.Contains(err.Error(), "\n") {
		t.Fatalf("expected a single-line message, got %q", err)
	}
}

func TestHeadCommitOutsideARepositorySaysThatInstead(t *testing.T) {
	// A different problem with a different fix, so it must not be collapsed
	// into "no commits yet".
	_, err := NewManager(t.TempDir(), nil).HeadCommit()
	if err != ErrNotARepository {
		t.Fatalf("expected ErrNotARepository, got %v", err)
	}
}

func TestHeadCommitResolvesNormally(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "initial")

	head, err := NewManager(root, nil).HeadCommit()
	if err != nil {
		t.Fatalf("HeadCommit: %v", err)
	}
	if len(strings.TrimSpace(head)) < 7 {
		t.Fatalf("expected a commit sha, got %q", head)
	}
}

func TestFreshnessOnAFreshRepoIsShort(t *testing.T) {
	// The end-to-end property: this string is what reaches the top of every
	// response, so its length is the thing that actually matters.
	root := t.TempDir()
	runGit(t, root, "init")

	freshness := NewManager(root, nil).Freshness("")
	if freshness.Unknown == "" {
		t.Fatalf("expected the situation reported")
	}
	if strings.Contains(freshness.Unknown, "\n") || len(freshness.Unknown) > 80 {
		t.Fatalf("expected a short single-line explanation, got %q", freshness.Unknown)
	}
}

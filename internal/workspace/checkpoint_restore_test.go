package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/julianbei/jade/internal/writes"
)

func readOrMissing(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "<missing>"
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestRevertRestoresExistenceAndAdvancesTheRevision(t *testing.T) {
	root := t.TempDir()
	m := NewManager(root, nil)
	for name, content := range map[string]string{"a.txt": "a1\n", "b.txt": "b1\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	checkpoint := m.Checkpoint("")

	// All three first touched after the checkpoint, through the write path.
	if err := writes.File(filepath.Join(root, "a.txt"), []byte("a2\n")); err != nil {
		t.Fatal(err)
	}
	m.BumpRevision("a.txt")
	if err := writes.Remove(filepath.Join(root, "b.txt")); err != nil {
		t.Fatal(err)
	}
	m.BumpRevision("b.txt")
	if err := writes.File(filepath.Join(root, "c.txt"), []byte("c\n")); err != nil {
		t.Fatal(err)
	}
	m.BumpRevision("c.txt")
	before := m.Revision()

	restored, err := m.RevertCheckpoint(checkpoint.ID)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"a.txt": "a1\n", "b.txt": "b1\n", "c.txt": "<missing>"} {
		if got := readOrMissing(t, filepath.Join(root, name)); got != want {
			t.Errorf("%s: got %q, want %q", name, got, want)
		}
	}
	if restored.Revision == checkpoint.Revision || restored.Revision == before || restored.Revision != m.Revision() {
		t.Fatalf("revert should create a new revision after %s, got %s (checkpoint %s, now %s)", before, restored.Revision, checkpoint.Revision, m.Revision())
	}
}

func TestRevertThatCannotRestoreEverythingChangesNothing(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the directory permission this test relies on")
	}
	root := t.TempDir()
	m := NewManager(root, nil)
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "b.txt"), []byte("b1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	checkpoint := m.Checkpoint("")

	if err := writes.File(filepath.Join(root, "a.txt"), []byte("a2\n")); err != nil {
		t.Fatal(err)
	}
	if err := writes.File(filepath.Join(sub, "b.txt"), []byte("b2\n")); err != nil {
		t.Fatal(err)
	}
	m.BumpRevision("a.txt", "sub/b.txt")
	before := m.Revision()

	if err := os.Chmod(sub, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(sub, 0o755)

	if _, err := m.RevertCheckpoint(checkpoint.ID); err == nil {
		t.Fatal("a revert that cannot write sub/b.txt should fail")
	}
	if got := readOrMissing(t, filepath.Join(root, "a.txt")); got != "a2\n" {
		t.Fatalf("a failed revert must not leave a.txt half-restored, got %q", got)
	}
	if m.Revision() != before {
		t.Fatalf("a failed revert must leave the revision at %s, got %s", before, m.Revision())
	}
}
func TestChangesRecordsRunsAndWhichFilesThisSessionEdited(t *testing.T) {
	root := t.TempDir()
	m := NewManager(root, nil)
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.BumpRevision("a.txt")
	m.RecordRun("check tests", "passed")
	m.RecordRun("command validate", "running")

	response := m.ChangesResponse()
	if len(response.Runs) != 1 || response.Runs[0].Kind != "check tests" || response.Runs[0].Revision != m.Revision() {
		t.Fatalf("expected one finished run at %s, got %+v", m.Revision(), response.Runs)
	}
	found := false
	for _, file := range response.Files {
		if file.Path == "a.txt" {
			found = true
			if file.By != "this session" {
				t.Fatalf("a file this session edited should say so, got %q", file.By)
			}
		}
	}
	if !found {
		t.Fatalf("a.txt missing from %+v", response.Files)
	}
}

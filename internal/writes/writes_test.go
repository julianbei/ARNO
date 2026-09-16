package writes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileKeepsPermissionsAndFollowsSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "script.sh")
	if err := os.WriteFile(target, []byte("old\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.sh")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if err := File(link, []byte("new\n")); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the link should stay a link, got %v %v", info.Mode(), err)
	}
	data, _ := os.ReadFile(target)
	if string(data) != "new\n" {
		t.Fatalf("the target should hold the new contents, got %q", data)
	}
	if info, _ := os.Stat(target); info.Mode().Perm() != 0o755 {
		t.Fatalf("permissions should be kept, got %v", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".arno-") {
			t.Fatalf("temporary file left behind: %s", entry.Name())
		}
	}
}

func TestFilesWritesAllOrNone(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(existing, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	created := filepath.Join(dir, "new.txt")
	unwritable := filepath.Join(dir, "missing", "b.txt")

	err := Files([]Change{
		{Path: existing, Data: []byte("changed\n")},
		{Path: created, Data: []byte("created\n")},
		{Path: unwritable, Data: []byte("never\n")},
	})
	if err == nil || !strings.Contains(err.Error(), "were restored") {
		t.Fatalf("expected a failure that restored the others, got %v", err)
	}
	if data, _ := os.ReadFile(existing); string(data) != "original\n" {
		t.Fatalf("an existing file should be restored, got %q", data)
	}
	if _, statErr := os.Stat(created); !os.IsNotExist(statErr) {
		t.Fatalf("a file the failed change created should be removed, got %v", statErr)
	}
}

func TestEveryObserverOfARootIsTold(t *testing.T) {
	root := t.TempDir()
	var first, second []string
	Observe(root, func(path string) { first = append(first, path) })
	Observe(root, func(path string) { second = append(second, path) })

	target := filepath.Join(root, "a.txt")
	if err := File(target, []byte("a\n")); err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || len(second) != 1 || first[0] != target || second[0] != target {
		t.Fatalf("both observers should be told of the write, got %v and %v", first, second)
	}
}

// TestEditsWriteOnlyThroughThisPackage keeps the write path single: an edit
// package that writes a file itself skips atomicity and all-or-nothing.
func TestEditsWriteOnlyThroughThisPackage(t *testing.T) {
	// Every package that changes workspace files: edits, the command registry
	// (.arno/commands.json), checkpoint restore and the API layer.
	for _, dir := range []string{"../code", "../edit", "../commands", "../workspace", "../transport/internalapi"} {
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			if strings.Contains(string(data), "os.WriteFile(") {
				t.Errorf("%s calls os.WriteFile; write through internal/writes", path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

package pathguard

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func workspace(t *testing.T) (root string, outside string) {
	t.Helper()
	base := t.TempDir()
	root = filepath.Join(base, "repo")
	outside = filepath.Join(base, "elsewhere")
	for _, dir := range []string{filepath.Join(root, "src"), outside} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, outside
}

func symlink(t *testing.T, target string, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks not available: %v", err)
	}
}

func TestInsidePathsResolve(t *testing.T) {
	root, _ := workspace(t)
	for _, path := range []string{
		"src/main.go",
		"./src/../src/main.go",
		"new/dir/file.go",
		filepath.Join(root, "src", "main.go"),
	} {
		got, err := Resolve(root, path)
		if err != nil {
			t.Errorf("%s: unexpected refusal: %v", path, err)
			continue
		}
		if !strings.HasPrefix(got, root) {
			t.Errorf("%s: resolved to %s, not under %s", path, got, root)
		}
	}
}

func TestEscapesAreRefusedAndNameTheRoot(t *testing.T) {
	root, outside := workspace(t)
	symlink(t, filepath.Join(outside, "secret.txt"), filepath.Join(root, "src", "link.txt"))
	symlink(t, outside, filepath.Join(root, "linkdir"))

	cases := map[string]string{
		"absolute outside":          filepath.Join(outside, "secret.txt"),
		"dot-dot escape":            "../elsewhere/secret.txt",
		"dot-dot after a directory": "src/../../elsewhere/secret.txt",
		"symlinked file":            "src/link.txt",
		"file in a symlinked dir":   "linkdir/secret.txt",
		"new file in symlinked dir": "linkdir/created.txt",
		"the parent directory":      "..",
	}
	for name, path := range cases {
		_, err := Resolve(root, path)
		if !errors.Is(err, ErrOutsideWorkspace) {
			t.Errorf("%s (%s): expected ErrOutsideWorkspace, got %v", name, path, err)
			continue
		}
		if !strings.Contains(err.Error(), root) {
			t.Errorf("%s: refusal should name the root, got: %v", name, err)
		}
	}
}

// A symlink that stays inside the workspace is ordinary repository structure.
func TestSymlinkInsideTheWorkspaceIsAllowed(t *testing.T) {
	root, _ := workspace(t)
	if err := os.WriteFile(filepath.Join(root, "src", "real.go"), []byte("package src\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	symlink(t, filepath.Join(root, "src", "real.go"), filepath.Join(root, "alias.go"))
	if _, err := Resolve(root, "alias.go"); err != nil {
		t.Fatalf("an inside symlink should resolve: %v", err)
	}
}

// The root itself reached through a symlink (/var and /private/var on macOS)
// must not make every absolute path look outside.
func TestAbsolutePathThroughTheRealRootIsInside(t *testing.T) {
	root, _ := workspace(t)
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Resolve(root, filepath.Join(real, "src", "main.go"))
	if err != nil {
		t.Fatalf("expected inside, got %v", err)
	}
	if got != filepath.Join(root, "src", "main.go") {
		t.Fatalf("expected the path respelled under root, got %s", got)
	}
}

func TestEmptyPathIsRefused(t *testing.T) {
	if _, err := Resolve(t.TempDir(), "  "); err == nil {
		t.Fatal("an empty path must not resolve to the root")
	}
}

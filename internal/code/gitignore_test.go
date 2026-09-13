package code

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/julianbei/jade/internal/events"
	"github.com/julianbei/jade/internal/protocol"
)

func TestGrepSkipsGitignoredFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		".gitignore":           "distribution/\n*.log\n",
		"source/ky.ts":         "export const marker = 1;\n",
		"distribution/ky.js":   "export const marker = 1;\n",
		"debug.log":            "marker\n",
		"tracked/kept.log.txt": "marker\n",
	} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}

	index := NewIndex(root, events.NewBus())
	response, err := index.Grep(protocol.GrepRequest{Query: "marker"})
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]bool{}
	for _, match := range response.Matches {
		paths[match.Path] = true
	}
	if !paths["source/ky.ts"] || !paths["tracked/kept.log.txt"] {
		t.Errorf("expected source matches, got %v", paths)
	}
	if paths["distribution/ky.js"] || paths["debug.log"] {
		t.Errorf("gitignored files must be skipped, got %v", paths)
	}
}

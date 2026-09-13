package edit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnippetShowsTheEditedRegionWithContext(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := "package a\n\nfunc A() {\n\treturn\n}\n\nfunc B() {\n\tprintln(\"b\")\n}\n\nfunc C() {}\n"
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	got := snippets(root, "a.go", "func B() {\n\tprintln(\"b\")\n}")
	if len(got) != 1 {
		t.Fatalf("expected one snippet, got %v", got)
	}
	if got[0].StartLine != 5 || got[0].EndLine != 11 {
		t.Errorf("expected lines 5-11, got %d-%d", got[0].StartLine, got[0].EndLine)
	}
	if !strings.Contains(got[0].Source, "func B() {") || !strings.Contains(got[0].Source, "func C() {}") {
		t.Errorf("expected the region and its context, got:\n%s", got[0].Source)
	}

	if braces := snippets(root, "a.go", "}\n"); braces != nil {
		t.Errorf("text with no unique line must report nothing, got %v", braces)
	}
}

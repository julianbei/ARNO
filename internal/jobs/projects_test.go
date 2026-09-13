package jobs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverProjectsFindsManifestsBelowTheRoot(t *testing.T) {
	root := t.TempDir()
	for path, content := range map[string]string{
		"go.mod":                               "module root\n",
		"services/api/go.mod":                  "module api\n",
		"services/api/internal/x/go.mod":       "module nested\n",
		"apps/web/package.json":                "{}\n",
		"apps/web/node_modules/p/package.json": "{}\n",
		"node_modules/q/package.json":          "{}\n",
		"tools/Cargo.toml":                     "[package]\n",
		".hidden/go.mod":                       "module hidden\n",
		"docs/readme.md":                       "# docs\n",
	} {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got := DiscoverProjects(root)
	want := []Project{
		{Path: "apps/web", Manifest: "package.json"},
		{Path: "services/api", Manifest: "go.mod"},
		{Path: "tools", Manifest: "Cargo.toml"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	}
}

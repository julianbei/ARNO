package code

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/julianbei/arno/internal/events"
	"github.com/julianbei/arno/internal/protocol"
)

func TestSearchesSkipPathsTheProjectMarksGenerated(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		".arno/project.json": `{"areas": [], "generated": ["api/gen", "*.pb.go"]}`,
		"api/gen/client.go":  "package gen // marker\n",
		"api/user.pb.go":     "package api // marker\n",
		"api/user.go":        "package api // marker\n",
	} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	response, err := NewIndex(root, events.NewBus()).Grep(protocol.GrepRequest{Query: "marker"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Total != 1 || response.Matches[0].Path != "api/user.go" {
		t.Errorf("expected only the hand-written file, got %+v", response.Matches)
	}
}

package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/julianbei/arno/internal/code"
	"github.com/julianbei/arno/internal/diagnostics"
	"github.com/julianbei/arno/internal/edit"
	"github.com/julianbei/arno/internal/events"
	"github.com/julianbei/arno/internal/jobs"
	"github.com/julianbei/arno/internal/languages"
	"github.com/julianbei/arno/internal/telemetry"
	"github.com/julianbei/arno/internal/transport/internalapi"
	"github.com/julianbei/arno/internal/workspace"
)

// newTestMCPServerAt is a second session on an existing workspace, with its
// own revision counter, index and checkpoints, the way a second agent's
// arno-mcp process would be.
func newTestMCPServerAt(t *testing.T, root string) *mcpServer {
	t.Helper()
	bus := events.NewBus()
	wm := workspace.NewManager(root, bus)
	ci := code.NewIndex(root, bus)
	ds := diagnostics.NewService(root)
	jr := jobs.NewRunner(bus)
	es := edit.NewService(wm, ci, ds, jr)
	api := internalapi.NewServer(wm, ci, es, ds, jr, languages.NewRegistry(), bus)
	return &mcpServer{api: api, telemetry: telemetry.New(root)}
}

func readFileText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestTwoSessionsOnOneWorkspaceDoNotLoseEachOthersWork(t *testing.T) {
	first, root := newTestMCPServer(t)
	second := newTestMCPServerAt(t, root)
	shared := filepath.Join(root, "shared.txt")
	writeWorkspaceFile(t, root, "shared.txt", "alpha\nbeta\n")

	read, err := callText(t, first, "arno.read_range", map[string]interface{}{"path": "shared.txt"})
	if err != nil {
		t.Fatal(err)
	}
	stale := digestHeader.FindStringSubmatch(read)
	if stale == nil {
		t.Fatalf("no digest in:\n%s", read)
	}

	// The second session edits the file the first one has read. The first
	// session's own revision counter does not move: only the digest sees it.
	if _, err := callText(t, second, "arno.replace_text", map[string]interface{}{"path": "shared.txt", "oldText": "beta", "newText": "BETA"}); err != nil {
		t.Fatal(err)
	}

	_, err = callText(t, first, "arno.replace_text", map[string]interface{}{
		"path": "shared.txt", "oldText": "alpha", "newText": "ALPHA", "expectedDigest": stale[1],
	})
	if err == nil || !strings.Contains(err.Error(), "changed since the read") {
		t.Fatalf("an edit based on a read the other session invalidated should be refused, got %v", err)
	}
	if text := readFileText(t, shared); !strings.Contains(text, "BETA") || strings.Contains(text, "ALPHA") {
		t.Fatalf("the refused edit must not write, and the other session's edit must stay:\n%s", text)
	}

	fresh, err := callText(t, first, "arno.read_range", map[string]interface{}{"path": "shared.txt"})
	if err != nil {
		t.Fatal(err)
	}
	current := digestHeader.FindStringSubmatch(fresh)
	if current == nil {
		t.Fatalf("no digest in:\n%s", fresh)
	}
	if _, err := callText(t, first, "arno.replace_text", map[string]interface{}{
		"path": "shared.txt", "oldText": "alpha", "newText": "ALPHA", "expectedDigest": current[1],
	}); err != nil {
		t.Fatal(err)
	}
	if text := readFileText(t, shared); !strings.Contains(text, "ALPHA") || !strings.Contains(text, "BETA") {
		t.Fatalf("both sessions' edits should be in the file:\n%s", text)
	}

	// Concurrent applies from both sessions, to different files, both land.
	writeWorkspaceFile(t, root, "a.txt", "a\n")
	writeWorkspaceFile(t, root, "b.txt", "b\n")
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, job := range []struct {
		server *mcpServer
		path   string
		old    string
	}{{first, "a.txt", "a"}, {second, "b.txt", "b"}} {
		wg.Add(1)
		go func(i int, server *mcpServer, path string, old string) {
			defer wg.Done()
			_, errs[i] = callText(t, server, "arno.apply", map[string]interface{}{
				"edits": []interface{}{
					map[string]interface{}{"op": "replace_text", "path": path, "oldText": old, "newText": strings.ToUpper(old)},
				},
			})
		}(i, job.server, job.path, job.old)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent apply %d failed: %v", i, err)
		}
	}
	if readFileText(t, filepath.Join(root, "a.txt")) != "A\n" || readFileText(t, filepath.Join(root, "b.txt")) != "B\n" {
		t.Fatal("both concurrent applies should land")
	}
}

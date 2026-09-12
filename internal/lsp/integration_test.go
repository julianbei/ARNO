package lsp

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// This exercises the client against a real language server rather than the
// fake one. It is skipped unless a server is actually installed, because the
// point is to catch what a hand-written fake cannot: a real server's startup
// sequence, its unprompted traffic, and the shape of its answers.
//
// gopls is the server used because jade's own tree is a Go workspace, so the
// test has a real codebase to answer about rather than a fixture.
func TestAgainstRealGopls(t *testing.T) {
	if testing.Short() {
		t.Skip("skipped in short mode: starts a language server and indexes a workspace")
	}
	spec, _ := SpecFor("go")
	if _, ok := spec.Resolve(); !ok {
		t.Skip("gopls is not installed")
	}

	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root = strings.TrimSuffix(root, "/internal/lsp")

	manager := NewManager(root)
	defer manager.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	client, ok := manager.ClientFor(ctx, "go")
	if !ok {
		t.Fatalf("gopls is installed but did not start: %s", manager.Unavailable("go"))
	}

	if !client.Supports("referencesProvider") || !client.Supports("renameProvider") {
		t.Fatalf("gopls must advertise references and rename, got %v/%v",
			client.Supports("referencesProvider"), client.Supports("renameProvider"))
	}

	const file = "internal/lsp/operations.go"
	if err := manager.Sync(client, file, "go"); err != nil {
		t.Fatalf("sync: %v", err)
	}

	// Find utf16Column's declaration in the file rather than hard-coding a
	// line: this test must not need editing every time the file above it
	// grows by a line.
	data, err := os.ReadFile(root + "/" + file)
	if err != nil {
		t.Fatal(err)
	}
	line, column, lineText := findDeclaration(string(data), "func utf16Column")
	if line == 0 {
		t.Fatal("could not locate utf16Column in its own file")
	}

	// A real server answers the first question only once it has indexed, so
	// retry rather than sleeping a guessed amount.
	var locations []Location
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		found, ok := References(ctx, client, root+"/"+file, lineText, line, column)
		if ok && len(found) > 0 {
			locations = found
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if len(locations) == 0 {
		t.Fatal("gopls returned no references for a function that is definitely used")
	}

	edits, err := Rename(ctx, client, root+"/"+file, lineText, line, column, "toUTF16Column")
	if err != nil {
		t.Fatalf("rename failed: %v", err)
	}
	if len(edits) < 2 {
		t.Fatalf("utf16Column is used from its test file too, so a rename must span at least 2 files, got %d", len(edits))
	}
	for _, fileEdit := range edits {
		for i := 1; i < len(fileEdit.Edits); i++ {
			previous, current := fileEdit.Edits[i-1], fileEdit.Edits[i]
			if current.StartLine > previous.StartLine {
				t.Fatalf("%s: edits must be ordered last-to-first so applying them does not invalidate the rest", fileEdit.Path)
			}
		}
	}
}

func findDeclaration(source string, declaration string) (line int, column int, text string) {
	for index, candidate := range strings.Split(source, "\n") {
		if offset := strings.Index(candidate, declaration); offset >= 0 {
			return index + 1, offset + len("func ") + 1, candidate
		}
	}
	return 0, 0, ""
}

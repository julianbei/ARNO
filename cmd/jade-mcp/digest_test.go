package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var digestHeader = regexp.MustCompile(`digest ([0-9a-f]{12})`)

func TestAnEditIsRefusedWhenTheFileChangedSinceItsRead(t *testing.T) {
	server, root := newTestMCPServer(t)
	writeWorkspaceFile(t, root, "a.txt", "one\ntwo\n")

	read, err := callText(t, server, "jade.read_range", map[string]interface{}{"path": "a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	stale := digestHeader.FindStringSubmatch(read)
	if stale == nil {
		t.Fatalf("a read should carry the file's digest, got:\n%s", read)
	}

	// Changed outside Jade: Jade's revision does not move.
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = callText(t, server, "jade.replace_text", map[string]interface{}{
		"path": "a.txt", "oldText": "two", "newText": "TWO", "expectedDigest": stale[1],
	})
	if err == nil || !strings.Contains(err.Error(), "changed since the read") {
		t.Fatalf("replace_text against a stale read should be refused, got %v", err)
	}
	_, err = callText(t, server, "jade.apply", map[string]interface{}{
		"edits": []interface{}{
			map[string]interface{}{"op": "replace_text", "path": "a.txt", "oldText": "two", "newText": "TWO", "expectedDigest": stale[1]},
		},
	})
	if err == nil {
		t.Fatal("apply against a stale read should be refused")
	}
	if data, _ := os.ReadFile(filepath.Join(root, "a.txt")); strings.Contains(string(data), "TWO") {
		t.Fatalf("a refused edit must not write, got:\n%s", data)
	}

	fresh, err := callText(t, server, "jade.read_range", map[string]interface{}{"path": "a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	current := digestHeader.FindStringSubmatch(fresh)
	if current == nil || current[1] == stale[1] {
		t.Fatalf("the digest should change with the file, got:\n%s", fresh)
	}
	if _, err := callText(t, server, "jade.replace_text", map[string]interface{}{
		"path": "a.txt", "oldText": "two", "newText": "TWO", "expectedDigest": current[1],
	}); err != nil {
		t.Fatalf("an edit against the current digest should apply: %v", err)
	}
}

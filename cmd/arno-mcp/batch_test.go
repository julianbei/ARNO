package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeWorkspaceFile(t *testing.T, root string, name string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func callText(t *testing.T, server *mcpServer, tool string, args map[string]interface{}) (string, error) {
	t.Helper()
	result, err := server.handleToolCall(toolCall(t, tool, args))
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, len(result.Content))
	for _, content := range result.Content {
		parts = append(parts, content.Text)
	}
	return strings.Join(parts, "\n"), nil
}

// Two or three declarations are usually needed together; one call answers all.
func TestFindAnswersSeveralNamesInOneCall(t *testing.T) {
	server, root := newTestMCPServer(t)
	writeWorkspaceFile(t, root, "store.go", "package store\n\ntype Project struct{}\n\ntype AttentionItem struct{}\n")

	out, err := callText(t, server, "arno.find", map[string]interface{}{
		"queries": []interface{}{"Project", "AttentionItem", "Missing"},
	})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	for _, want := range []string{"type Project struct{}", "type AttentionItem struct{}", "Missing:"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in:\n%s", want, out)
		}
	}
	if strings.Index(out, "Project struct") > strings.Index(out, "AttentionItem struct") {
		t.Errorf("answers should follow the order asked:\n%s", out)
	}
}

func TestFindWithNeitherQueryNorQueriesSaysWhatItNeeds(t *testing.T) {
	server, _ := newTestMCPServer(t)
	_, err := server.handleToolCall(toolCall(t, "arno.find", map[string]interface{}{}))
	if err == nil || !strings.Contains(err.Error(), "queries") {
		t.Fatalf("expected an error naming query and queries, got %v", err)
	}
}

// One mistyped path must not discard the good reads beside it.
func TestReadRangeReadsSeveralRangesAndIsolatesFailures(t *testing.T) {
	server, root := newTestMCPServer(t)
	writeWorkspaceFile(t, root, "a.txt", "a1\na2\na3\n")
	writeWorkspaceFile(t, root, "b.txt", "b1\nb2\n")

	out, err := callText(t, server, "arno.read_range", map[string]interface{}{
		"ranges": []interface{}{
			map[string]interface{}{"path": "a.txt", "startLine": 2, "endLine": 3},
			map[string]interface{}{"path": "nope.txt"},
			map[string]interface{}{"path": "b.txt", "startLine": 1, "endLine": 99},
		},
	})
	if err != nil {
		t.Fatalf("read_range: %v", err)
	}
	for _, want := range []string{"a.txt:2-3\na2\na3", "nope.txt:", "b.txt:1-2 (end of file, 2 lines)\nb1\nb2"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in:\n%s", want, out)
		}
	}
}

// The case that cost a turn four times in one session.
func TestReadRangeClampsAnEndPastTheFileAndSaysSo(t *testing.T) {
	server, root := newTestMCPServer(t)
	writeWorkspaceFile(t, root, "refs.go", "package refs\n\nfunc A() {}\n")

	out, err := callText(t, server, "arno.read_range", map[string]interface{}{
		"path": "refs.go", "startLine": 2, "endLine": 360,
	})
	if err != nil {
		t.Fatalf("expected the end clamped, got error: %v", err)
	}
	if !strings.Contains(strings.SplitN(out, "\n", 2)[0], "lines 2-3 of 3") {
		t.Fatalf("expected the clamp named in the header, got:\n%s", out)
	}
	if !strings.Contains(out, "func A() {}") {
		t.Fatalf("expected the content to the end of the file, got:\n%s", out)
	}
}

func TestReadRangeStartPastTheFileIsStillAnError(t *testing.T) {
	server, root := newTestMCPServer(t)
	writeWorkspaceFile(t, root, "short.txt", "one\n")
	if _, err := callText(t, server, "arno.read_range", map[string]interface{}{"path": "short.txt", "startLine": 5}); err == nil {
		t.Fatal("a start past the end has nothing to read and must be an error")
	}
}

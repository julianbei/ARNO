package main

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

var continueHandle = regexp.MustCompile(`continue=(c\d+)`)

func writeNeedles(t *testing.T, root string) {
	t.Helper()
	var b strings.Builder
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&b, "needle %02d\n", i)
	}
	writeWorkspaceFile(t, root, "a.txt", b.String())
}

func TestGrepBudgetPagesWithContinue(t *testing.T) {
	server, root := newTestMCPServer(t)
	writeNeedles(t, root)

	first, err := callText(t, server, "jade.grep", map[string]interface{}{"query": "needle", "budget": 40})
	if err != nil {
		t.Fatal(err)
	}
	handle := continueHandle.FindStringSubmatch(first)
	if handle == nil || !strings.Contains(first, "30 matches in 1 files, shown 1-") || strings.Contains(first, "needle 30") {
		t.Fatalf("expected a first page with a continue handle, got:\n%s", first)
	}

	pages := []string{first}
	next := handle[1]
	for i := 0; i < 30 && next != ""; i++ {
		page, err := callText(t, server, "jade.grep", map[string]interface{}{"continue": next})
		if err != nil {
			t.Fatalf("continue=%s: %v", next, err)
		}
		pages = append(pages, page)
		next = ""
		if m := continueHandle.FindStringSubmatch(page); m != nil {
			next = m[1]
		}
	}
	all := strings.Join(pages, "\n")
	for i := 1; i <= 30; i++ {
		if !strings.Contains(all, fmt.Sprintf("needle %02d", i)) {
			t.Fatalf("needle %02d was never shown across the pages:\n%s", i, all)
		}
	}
	if last := pages[len(pages)-1]; !strings.Contains(last, "the last") || !strings.Contains(last, "complete") {
		t.Fatalf("the last page should say so and be complete, got:\n%s", last)
	}
}

func TestGrepContinueRefusedAfterAnEdit(t *testing.T) {
	server, root := newTestMCPServer(t)
	writeNeedles(t, root)

	first, err := callText(t, server, "jade.grep", map[string]interface{}{"query": "needle", "budget": 40})
	if err != nil {
		t.Fatal(err)
	}
	handle := continueHandle.FindStringSubmatch(first)
	if handle == nil {
		t.Fatalf("no continue handle in:\n%s", first)
	}
	if _, err := callText(t, server, "jade.replace_text", map[string]interface{}{"path": "a.txt", "oldText": "needle 01", "newText": "needle 00"}); err != nil {
		t.Fatal(err)
	}
	if _, err := callText(t, server, "jade.grep", map[string]interface{}{"continue": handle[1]}); err == nil || !strings.Contains(err.Error(), "repeat the call") {
		t.Fatalf("a handle cut before an edit must be refused, got %v", err)
	}
}

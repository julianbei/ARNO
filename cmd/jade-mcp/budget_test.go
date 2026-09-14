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
func TestFindBudgetPagesWithContinue(t *testing.T) {
	server, root := newTestMCPServer(t)
	var b strings.Builder
	b.WriteString("package a\n\n")
	for i := 1; i <= 20; i++ {
		fmt.Fprintf(&b, "func Needle%02d() {\n\t_ = %d\n}\n\n", i, i)
	}
	writeWorkspaceFile(t, root, "a.go", b.String())

	page, err := callText(t, server, "jade.find", map[string]interface{}{"query": "Needle", "budget": 60})
	if err != nil {
		t.Fatal(err)
	}
	all := page
	for i := 0; i < 40; i++ {
		m := continueHandle.FindStringSubmatch(page)
		if m == nil {
			break
		}
		if page, err = callText(t, server, "jade.find", map[string]interface{}{"continue": m[1]}); err != nil {
			t.Fatal(err)
		}
		all += "\n" + page
	}
	if !strings.Contains(all, "continue=") {
		t.Fatalf("expected the first page to be cut:\n%s", all)
	}
	for i := 1; i <= 20; i++ {
		if !strings.Contains(all, fmt.Sprintf("Needle%02d", i)) {
			t.Fatalf("Needle%02d was never shown:\n%s", i, all)
		}
	}
	if !strings.Contains(page, "the last") {
		t.Fatalf("the last page should say so, got:\n%s", page)
	}
}

func TestReferencesBudgetPagesWithContinue(t *testing.T) {
	server, root := newTestMCPServer(t)
	var b strings.Builder
	b.WriteString("package a\n\nfunc Target() {}\n\n")
	for i := 1; i <= 12; i++ {
		fmt.Fprintf(&b, "func Caller%02d() {\n\tTarget()\n}\n\n", i)
	}
	writeWorkspaceFile(t, root, "a.go", b.String())

	page, err := callText(t, server, "jade.references", map[string]interface{}{"path": "a.go", "symbolName": "Target", "budget": 20})
	if err != nil {
		t.Fatal(err)
	}
	all := page
	for i := 0; i < 40; i++ {
		m := continueHandle.FindStringSubmatch(page)
		if m == nil {
			break
		}
		if page, err = callText(t, server, "jade.references", map[string]interface{}{"continue": m[1]}); err != nil {
			t.Fatal(err)
		}
		all += "\n" + page
	}
	if !strings.Contains(all, "continue=") {
		t.Fatalf("expected the first page to be cut:\n%s", all)
	}
	count := 0
	for _, line := range strings.Split(all, "\n") {
		if strings.HasPrefix(line, "a.go") {
			count++
		}
	}
	if count < 12 {
		t.Fatalf("expected all 12 references across the pages, got %d:\n%s", count, all)
	}
}
func TestReadRangePagesALargeFileInsteadOfCuttingItsMiddle(t *testing.T) {
	server, root := newTestMCPServer(t)
	var b strings.Builder
	for i := 1; i <= 3000; i++ {
		fmt.Fprintf(&b, "line %04d padding padding padding\n", i)
	}
	writeWorkspaceFile(t, root, "big.txt", b.String())

	page, err := callText(t, server, "jade.read_range", map[string]interface{}{"path": "big.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(page, "bytes omitted") || !strings.Contains(page, "lines 1-") || !strings.Contains(page, "continue=") {
		t.Fatalf("a large file should be paged at whole lines with a handle, got the head:\n%.300s", page)
	}
	all := page
	for i := 0; i < 20; i++ {
		m := continueHandle.FindStringSubmatch(page)
		if m == nil {
			break
		}
		if page, err = callText(t, server, "jade.read_range", map[string]interface{}{"continue": m[1]}); err != nil {
			t.Fatal(err)
		}
		all += "\n" + page
	}
	for _, want := range []string{"line 0001 ", "line 1500 ", "line 3000 "} {
		if !strings.Contains(all, want) {
			t.Fatalf("%q never read across the pages", want)
		}
	}
	if continueHandle.MatchString(page) {
		t.Fatalf("the last page should carry no handle:\n%.300s", page)
	}
}
func TestWorkspaceTreeBudgetPagesWithContinue(t *testing.T) {
	server, root := newTestMCPServer(t)
	for i := 1; i <= 40; i++ {
		writeWorkspaceFile(t, root, fmt.Sprintf("pkg/file%02d.txt", i), "x\n")
	}

	page, err := callText(t, server, "jade.workspace_tree", map[string]interface{}{"budget": 20})
	if err != nil {
		t.Fatal(err)
	}
	all := page
	for i := 0; i < 60; i++ {
		m := continueHandle.FindStringSubmatch(page)
		if m == nil {
			break
		}
		if page, err = callText(t, server, "jade.workspace_tree", map[string]interface{}{"continue": m[1]}); err != nil {
			t.Fatal(err)
		}
		all += "\n" + page
	}
	if !strings.Contains(all, "continue=") {
		t.Fatalf("expected the listing to be cut:\n%s", all)
	}
	for i := 1; i <= 40; i++ {
		if !strings.Contains(all, fmt.Sprintf("pkg/file%02d.txt", i)) {
			t.Fatalf("file%02d never listed:\n%s", i, all)
		}
	}
}
func TestReadRangesPageALargeRangeWithAReadRangeHandle(t *testing.T) {
	server, root := newTestMCPServer(t)
	var b strings.Builder
	for i := 1; i <= 3000; i++ {
		fmt.Fprintf(&b, "line %04d padding padding padding\n", i)
	}
	writeWorkspaceFile(t, root, "big.txt", b.String())
	writeWorkspaceFile(t, root, "small.txt", "one\ntwo\n")

	out, err := callText(t, server, "jade.read_range", map[string]interface{}{
		"ranges": []interface{}{
			map[string]interface{}{"path": "big.txt"},
			map[string]interface{}{"path": "small.txt"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	m := continueHandle.FindStringSubmatch(out)
	if m == nil || strings.Contains(out, "bytes omitted") || !strings.Contains(out, "two") {
		t.Fatalf("expected the big range paged with a handle and the small one whole, got:\n%.400s", out)
	}
	rest, err := callText(t, server, "jade.read_range", map[string]interface{}{"continue": m[1]})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "line 3000 ") == strings.Contains(rest, "line 3000 ") && !strings.Contains(rest, "continue=") {
		t.Fatalf("the handle should continue the big range, got:\n%.400s", rest)
	}
}

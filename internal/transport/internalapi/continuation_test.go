package internalapi

import "testing"

func TestLinePageCutsAtWholeLines(t *testing.T) {
	lines := []string{"aaaa", "bbbb", "cccc", "dddd"}
	if end := linePage(lines, 0, 10); end != 2 {
		t.Fatalf("two 5-byte lines fit 10 bytes, got end %d", end)
	}
	if end := linePage(lines, 2, 10); end != 4 {
		t.Fatalf("the rest fits, got end %d", end)
	}
	if end := linePage([]string{"a very long line"}, 0, 3); end != 1 {
		t.Fatalf("a page holds at least one line, got end %d", end)
	}
	if end := linePage(lines, 4, 10); end != 4 {
		t.Fatalf("a page from the end is empty, got end %d", end)
	}
}

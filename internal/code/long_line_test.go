package code

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// longLine is past bufio.MaxScanTokenSize (64 KiB), the limit the old
// scanner-based splitLines stopped at without reporting an error.
func longLine() string {
	return "const SHADER = `" + strings.Repeat("x", 70000) + "`;"
}

func writeFixture(t *testing.T, name string, content string) (root string, path string) {
	t.Helper()
	root = t.TempDir()
	path = filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, path
}

// A line longer than the scanner's token limit used to end the scan silently,
// so every line after it did not exist as far as ARNO was concerned.
func TestSplitLinesKeepsEverythingAfterAVeryLongLine(t *testing.T) {
	source := "first\n" + longLine() + "\nlast\n"

	lines := splitLines(source)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d — the long line truncated the file", len(lines))
	}
	if lines[2] != "last" {
		t.Errorf("the line after the long one is %q", lines[2])
	}
	if lines[1] != longLine() {
		t.Errorf("the long line came back %d bytes instead of %d", len(lines[1]), len(longLine()))
	}
}

// The data-loss case from issue #5: an edit far from the long line used to
// write back only the truncated prefix and report success.
func TestReplaceRangeKeepsContentAfterAVeryLongLine(t *testing.T) {
	source := "a\nb\nc\n" + longLine() + "\ntail1\ntail2\n"
	root, path := writeFixture(t, "big.ts", source)

	index := NewIndex(root, nil)
	if _, err := index.ReplaceRangeSource("big.ts", 2, 2, "replaced"); err != nil {
		t.Fatalf("replace_range: %v", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(after)
	if !strings.Contains(got, longLine()) {
		t.Error("the long line was dropped")
	}
	if !strings.Contains(got, "tail2") {
		t.Error("everything after the long line was deleted, and the edit reported success")
	}
	if want := "a\nreplaced\nc\n" + longLine() + "\ntail1\ntail2\n"; got != want {
		t.Errorf("file is %d bytes, expected %d", len(got), len(want))
	}
}

// A three-line edit must not rewrite every line of a CRLF file.
func TestReplaceRangeKeepsCRLFLineEndings(t *testing.T) {
	lines := []string{"const a = 1;", "const b = 2;", "const c = 3;", "const d = 4;"}
	source := strings.Join(lines, "\r\n") + "\r\n"
	root, path := writeFixture(t, "crlf.ts", source)

	index := NewIndex(root, nil)
	if _, err := index.ReplaceRangeSource("crlf.ts", 2, 2, "const b = 20;"); err != nil {
		t.Fatalf("replace_range: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "const a = 1;\r\nconst b = 20;\r\nconst c = 3;\r\nconst d = 4;\r\n"; string(got) != want {
		t.Errorf("CRLF file was rewritten:\n got %q\nwant %q", got, want)
	}
}

// A file that ended without a newline keeps ending without one: adding it
// shows up as a change to a line the edit never named.
func TestReplaceRangeKeepsAMissingTrailingNewline(t *testing.T) {
	root, path := writeFixture(t, "nonewline.ts", "one\ntwo\nthree")

	index := NewIndex(root, nil)
	if _, err := index.ReplaceRangeSource("nonewline.ts", 1, 1, "ONE"); err != nil {
		t.Fatalf("replace_range: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "ONE\ntwo\nthree" {
		t.Errorf("trailing newline state changed: %q", got)
	}
}

// Issue #5's third symptom: the anchor was copied from a read and verified
// against the file, and the tool still said it was not there, because every
// read strips "\r" and the file is CRLF.
func TestReplaceTextMatchesAnLFAnchorAgainstACRLFFile(t *testing.T) {
	source := "const a = 1;\r\nconst b = 2;\r\nconst c = 3;\r\n"
	root, path := writeFixture(t, "crlf.ts", source)

	index := NewIndex(root, nil)
	// What a read hands back: the same text with "\r" stripped.
	anchor := "const a = 1;\nconst b = 2;"
	if _, err := index.ReplaceTextSource("crlf.ts", anchor, "const ab = 12;"); err != nil {
		t.Fatalf("an anchor copied from a read should match the file it came from: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "const ab = 12;\r\nconst c = 3;\r\n"; string(got) != want {
		t.Errorf("the replacement did not keep the file's line endings:\n got %q\nwant %q", got, want)
	}
}

// An anchor that is genuinely absent must still be refused, rather than
// matched by the line-ending conversion.
func TestReplaceTextStillRefusesAnAbsentAnchor(t *testing.T) {
	root, _ := writeFixture(t, "crlf.ts", "const a = 1;\r\nconst b = 2;\r\n")

	index := NewIndex(root, nil)
	if _, err := index.ReplaceTextSource("crlf.ts", "const q = 9;\nconst r = 8;", "x"); err == nil {
		t.Fatal("an anchor that is not in the file must be refused")
	}
}

// The hint pins the anchor on one unique line and reports where the two part.
// That alignment is a guess, and saying so is the difference between a useful
// hint and the misleading one issue #5 reported.
func TestAnchorHintSaysWhatItsGuessRestsOn(t *testing.T) {
	source := "alpha\nbeta\nuniqueMarkerLine\ngamma\ndelta\n"
	hint := AnchorHint(source, "uniqueMarkerLine\nNOT THE NEXT LINE")

	if !strings.Contains(hint, "uniqueMarkerLine") {
		t.Errorf("the hint should name the line it aligned on, got: %s", hint)
	}
	if !strings.Contains(hint, "alignment is wrong") {
		t.Errorf("the hint should admit the alignment is a guess, got: %s", hint)
	}
}

package code

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// ErrTextNotFound is returned when the anchor text does not appear in the
// file at all.
var ErrTextNotFound = errors.New("anchor text not found")

// ErrTextAmbiguous is returned when the anchor text appears more than once.
// Editing the first match would be a coin flip on which one the caller
// meant, so ARNO refuses and says how many it found — the caller can then
// extend the anchor with surrounding context to disambiguate.
var ErrTextAmbiguous = errors.New("anchor text is ambiguous")

// ReplaceTextSource replaces an exact, unique string in a file.
//
// This exists because line numbers are the wrong address for a sequence of
// edits: every prior edit shifts them, so each follow-up edit needs a fresh
// read first. Dogfooding ARNO through its own development produced several
// off-by-N splices from exactly that — a case landing inside a struct
// literal, another replacing an import line instead of inserting above it.
// An anchor string does not move when the lines around it do.
//
// Uniqueness is required rather than preferred. "Replace the first match" is
// the behavior that makes sed dangerous in a script, and an agent cannot see
// which match it got.
func (i *Index) ReplaceTextSource(path string, oldText string, newText string) (int, error) {
	if oldText == "" {
		return 0, fmt.Errorf("anchor text is required")
	}

	absolute, err := i.resolvePath(path)
	if err != nil {
		return 0, err
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		return 0, err
	}

	source := string(data)
	oldText, newText = matchLineEndings(source, oldText, newText)
	count := strings.Count(source, oldText)
	switch count {
	case 0:
		return 0, fmt.Errorf("%w in %s: %s", ErrTextNotFound, path, AnchorHint(source, oldText))
	case 1:
	default:
		return 0, fmt.Errorf("%w: %d matches in %s — extend the anchor with surrounding context", ErrTextAmbiguous, count, path)
	}

	updated := strings.Replace(source, oldText, newText, 1)
	if err := writeFile(absolute, []byte(updated)); err != nil {
		return 0, err
	}

	// The symbol cache keys on mtime and size, so the write self-invalidates
	// and nothing has to be evicted here.
	return countLinesIn(oldText), nil
}

// countLinesIn counts the lines a chunk of text occupies. The trailing
// newline is trimmed first: "func B() {}\n" is one line, not two, and
// counting it as two made every insert and replacement report one line more
// than it actually wrote.
func countLinesIn(text string) int {
	trimmed := strings.TrimSuffix(text, "\n")
	if trimmed == "" {
		return 0
	}
	return strings.Count(trimmed, "\n") + 1
}

// matchLineEndings rewrites a multi-line anchor and its replacement to the
// line endings the file actually uses, when the anchor would otherwise match
// nothing.
//
// Every read path strips "\r" (see splitLines), so an agent that copies an
// anchor out of a read and sends it back is holding LF text for a CRLF file.
// strings.Count then found nothing, and the refusal said "anchor text not
// found" about text the caller had just verified byte for byte against the
// file — the one message guaranteed to send it looking in the wrong place.
//
// The conversion is attempted only when the exact anchor is absent and the
// converted one is present, so a file with mixed endings, or an anchor that
// is genuinely wrong, is left to fail as before. The replacement is converted
// with the anchor: inserting LF text into a CRLF file is how a mixed file
// gets made.
func matchLineEndings(source string, oldText string, newText string) (string, string) {
	if !strings.Contains(oldText, "\n") || strings.Contains(source, oldText) {
		return oldText, newText
	}

	toCRLF := func(text string) string {
		return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", "\r\n")
	}
	toLF := func(text string) string {
		return strings.ReplaceAll(text, "\r\n", "\n")
	}

	for _, convert := range []func(string) string{toCRLF, toLF} {
		if converted := convert(oldText); converted != oldText && strings.Contains(source, converted) {
			return converted, convert(newText)
		}
	}
	return oldText, newText
}

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
// meant, so arno refuses and says how many it found — the caller can then
// extend the anchor with surrounding context to disambiguate.
var ErrTextAmbiguous = errors.New("anchor text is ambiguous")

// ReplaceTextSource replaces an exact, unique string in a file.
//
// This exists because line numbers are the wrong address for a sequence of
// edits: every prior edit shifts them, so each follow-up edit needs a fresh
// read first. Dogfooding arno through its own development produced several
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

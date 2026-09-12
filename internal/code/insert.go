package code

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// InsertPosition says where text goes relative to an anchor.
const (
	InsertEnd    = "end"
	InsertStart  = "start"
	InsertBefore = "before"
	InsertAfter  = "after"
)

// ErrAnchorRequired is returned when a positional insert names no anchor.
var ErrAnchorRequired = errors.New("anchor is required for before/after inserts")

// InsertSource adds text to a file without replacing anything.
//
// jade could create files and replace text, but not append: `create_file`
// refuses to overwrite and `replace_text` needs something to replace, so
// adding a test to an existing file meant reading it first purely to learn
// what to anchor on. That round trip is why `cat >> file` kept winning.
//
// Anchors follow replace_text's rule — exactly one match or refuse — because
// inserting next to an arbitrary one of several matches is the same coin
// flip, silently placing code somewhere the caller did not look.
func (i *Index) InsertSource(path string, anchor string, position string, text string) (int, error) {
	if text == "" {
		return 0, fmt.Errorf("insert text is required")
	}

	absolute := i.resolvePath(path)
	data, err := os.ReadFile(absolute)
	if err != nil {
		return 0, err
	}
	source := string(data)

	updated, err := insertInto(source, anchor, position, text)
	if err != nil {
		return 0, fmt.Errorf("%w in %s", err, path)
	}
	if err := os.WriteFile(absolute, []byte(updated), 0o644); err != nil {
		return 0, err
	}

	return countLinesIn(text), nil
}

func insertInto(source string, anchor string, position string, text string) (string, error) {
	// An anchor with no position used to fall through to the append branch,
	// which ignored the anchor and put the text at the end of the file — the
	// exact silent misplacement the anchor exists to prevent. Supplying an
	// anchor is an instruction about where the text goes, so the only safe
	// readings are "after it" or an error, never "somewhere else entirely".
	if position == "" && anchor != "" {
		position = InsertAfter
	}

	switch position {
	case "", InsertEnd:
		return joinWithNewline(source, text), nil
	case InsertStart:
		return joinWithNewline(text, source), nil
	case InsertBefore, InsertAfter:
		if anchor == "" {
			return "", ErrAnchorRequired
		}
	default:
		return "", fmt.Errorf("unknown insert position %q (want end, start, before or after)", position)
	}

	switch strings.Count(source, anchor) {
	case 0:
		return "", ErrTextNotFound
	case 1:
	default:
		return "", fmt.Errorf("%w: %d matches — extend the anchor with surrounding context",
			ErrTextAmbiguous, strings.Count(source, anchor))
	}

	index := strings.Index(source, anchor)
	if position == InsertAfter {
		index += len(anchor)
		return source[:index] + ensureLeadingNewline(text) + source[index:], nil
	}
	return source[:index] + ensureTrailingNewline(text) + source[index:], nil
}

// joinWithNewline concatenates two chunks with exactly one newline between
// them, so an append never glues onto the previous line and never doubles a
// blank the caller did not ask for.
func joinWithNewline(first string, second string) string {
	first = strings.TrimRight(first, "\n")
	second = strings.TrimLeft(second, "\n")
	joined := first + "\n" + second
	if !strings.HasSuffix(joined, "\n") {
		joined += "\n"
	}
	return joined
}

func ensureLeadingNewline(text string) string {
	if strings.HasPrefix(text, "\n") {
		return text
	}
	return "\n" + text
}

func ensureTrailingNewline(text string) string {
	if strings.HasSuffix(text, "\n") {
		return text
	}
	return text + "\n"
}

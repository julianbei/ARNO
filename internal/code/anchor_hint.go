package code

import (
	"fmt"
	"strings"
)

// maxHintQuote bounds each line quoted in an anchor hint.
const maxHintQuote = 80

// AnchorHint says how an anchor that matched nothing differs from the file.
//
// "anchor text not found" alone left an agent in a cobra benchmark run
// retrying six rewritten anchors from memory: it had written
// `return c.displayName()` above a function where the file said
// `return c.Name()`, and nothing told it which line was wrong. The hint names
// that line, or says the text is there with different whitespace, so one
// corrected call replaces a run of guesses.
func AnchorHint(source string, anchor string) string {
	anchorLines := strings.Split(strings.TrimRight(anchor, "\n"), "\n")
	fileLines := strings.Split(source, "\n")

	// Only an anchor with some substance can be "there with different
	// spacing": a lone brace matches half the file that way.
	substantial := len(normalizeSpace(strings.Join(anchorLines, " "))) >= 4
	for start := 0; substantial && start+len(anchorLines) <= len(fileLines); start++ {
		matched := true
		for offset, line := range anchorLines {
			if normalizeSpace(fileLines[start+offset]) != normalizeSpace(line) {
				matched = false
				break
			}
		}
		if matched {
			return fmt.Sprintf("the text is at line %d with different indentation or spacing — copy the anchor from a read", start+1)
		}
	}

	counts := map[string]int{}
	positions := map[string]int{}
	for index, line := range fileLines {
		normalized := normalizeSpace(line)
		counts[normalized]++
		positions[normalized] = index
	}
	// Align on the first anchor line that occurs exactly once in the file, and
	// report the first line where the two part.
	for offset, line := range anchorLines {
		normalized := normalizeSpace(line)
		if len(normalized) < 4 || counts[normalized] != 1 {
			continue
		}
		start := positions[normalized] - offset
		for j, expected := range anchorLines {
			at := start + j
			if at < 0 || at >= len(fileLines) {
				return fmt.Sprintf("the anchor's line %q is at line %d, but the anchor does not fit around it", clipHint(line), positions[normalized]+1)
			}
			if normalizeSpace(fileLines[at]) != normalizeSpace(expected) {
				return fmt.Sprintf("line %d reads %q where the anchor has %q", at+1, clipHint(fileLines[at]), clipHint(expected))
			}
		}
	}
	return "no line of the anchor occurs exactly once in the file — read the region and copy the anchor from it"
}

func normalizeSpace(line string) string {
	return strings.Join(strings.Fields(line), " ")
}

func clipHint(line string) string {
	line = strings.TrimSpace(line)
	if len(line) > maxHintQuote {
		return line[:maxHintQuote] + "…"
	}
	return line
}

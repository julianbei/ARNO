package edit

import (
	"os"
	"strings"

	"github.com/julianbei/arno/internal/pathguard"
	"github.com/julianbei/arno/internal/protocol"
)

// snippetContext is how many unchanged lines frame an edited region.
const snippetContext = 2

// maxSnippetLines bounds one snippet; a longer region shows its head and tail.
const maxSnippetLines = 16

// maxSnippets bounds how many regions one apply reports.
const maxSnippets = 4

// snippets returns the region text landed in, as the file reads after the edit
// and its formatting, framed by a little context.
//
// An edit response that says only "+22 -20" leaves the caller unable to see
// what the file now looks like, so it reads the region back: in a benchmark
// run an agent re-read edited ranges nine times in one task, a turn each. The
// host's own edit tool returns the edited lines, and agents work that way.
//
// The region is found by the first line of text that occurs exactly once in
// the file, so formatting that re-indents the text still locates it. Text with
// no such line (only braces, say) reports nothing rather than a wrong place.
func snippets(root string, path string, text string) []protocol.Snippet {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	absolute, err := pathguard.Resolve(root, path)
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		return nil
	}
	fileLines := strings.Split(string(data), "\n")
	textLines := strings.Split(strings.TrimRight(text, "\n"), "\n")

	begin := -1
	for offset, line := range textLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if at, unique := uniqueLine(fileLines, trimmed); unique {
			begin = at - offset
			break
		}
	}
	if begin < 0 {
		return nil
	}

	from := begin - snippetContext
	if from < 0 {
		from = 0
	}
	to := begin + len(textLines) + snippetContext
	if to > len(fileLines) {
		to = len(fileLines)
	}
	selected := fileLines[from:to]
	source := strings.Join(selected, "\n")
	if len(selected) > maxSnippetLines {
		half := maxSnippetLines / 2
		source = strings.Join(selected[:half], "\n") + "\n…\n" + strings.Join(selected[len(selected)-half:], "\n")
	}
	return []protocol.Snippet{{Path: path, StartLine: from + 1, EndLine: to, Source: source}}
}

func uniqueLine(lines []string, trimmed string) (int, bool) {
	at := -1
	for index, line := range lines {
		if strings.TrimSpace(line) != trimmed {
			continue
		}
		if at >= 0 {
			return 0, false
		}
		at = index
	}
	return at, at >= 0
}

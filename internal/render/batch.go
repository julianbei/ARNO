package render

import (
	"fmt"
	"strings"

	"github.com/julianbei/jade/internal/protocol"
)

// findBatch renders several find answers one after another. Each keeps its own
// grep-style location headers, so the names asked for are identifiable without
// a separate label; a name with no match says so on its own line.
func findBatch(r protocol.FindBatchResponse) string {
	parts := make([]string, 0, len(r.Responses))
	for _, response := range r.Responses {
		text := find(response)
		if len(response.Results) == 0 {
			text = fmt.Sprintf("%s: %s", response.Query, text)
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, "\n\n")
}

// grepBatch renders several grep answers one after another, each labelled with
// its pattern. A pattern with no match already names itself ("no matches for
// ..."), so it is not labelled twice.
func grepBatch(r protocol.GrepBatchResponse) string {
	if len(r.Responses) == 0 {
		return "no patterns"
	}
	parts := make([]string, 0, len(r.Responses))
	for _, response := range r.Responses {
		text := grep(response)
		if response.Total > 0 {
			text = response.Query + ": " + text
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, "\n\n")
}

// readRanges renders each range under a `path:start-end` header, noting when
// the end was clamped to the end of the file and giving a failed range its
// error in place.
func readRanges(r protocol.ReadRangesResponse) string {
	lines := make([]string, 0, len(r.Results)*2+1)
	if r.Revision != "" {
		lines = append(lines, r.Revision)
	}
	for index, result := range r.Results {
		if index > 0 || r.Revision != "" {
			lines = append(lines, "")
		}
		if result.Error != "" {
			lines = append(lines, fmt.Sprintf("%s: %s", result.Path, result.Error))
			continue
		}
		header := fmt.Sprintf("%s:%d-%d", result.Path, result.StartLine, result.EndLine)
		if result.Clamped {
			header += fmt.Sprintf(" (end of file, %d lines)", result.TotalLines)
		}
		if result.Continue != "" {
			header += fmt.Sprintf(" of %d · continue=%s", result.TotalLines, result.Continue)
		}
		lines = append(lines, header, result.Source)
	}
	return strings.Join(lines, "\n")
}

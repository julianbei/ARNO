package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/julianbei/jade/internal/protocol"
)

// stringsArg reads an array-of-strings argument, skipping non-string entries.
func stringsArg(args map[string]interface{}, key string) []string {
	raw, ok := args[key].([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

// rangesArg decodes read_range's ranges array. It round-trips through JSON,
// like editOpsArg, so field names match the schema without hand-unpacking.
func rangesArg(args map[string]interface{}) ([]protocol.ReadRangeRequest, error) {
	encoded, err := json.Marshal(args["ranges"])
	if err != nil {
		return nil, fmt.Errorf("invalid ranges: %w", err)
	}
	var entries []struct {
		Path      string
		Lines     string
		StartLine int
		EndLine   int
	}
	if err := json.Unmarshal(encoded, &entries); err != nil {
		return nil, fmt.Errorf("invalid ranges: each entry is {path, lines}: %w", err)
	}
	ranges := make([]protocol.ReadRangeRequest, 0, len(entries))
	for _, entry := range entries {
		request := protocol.ReadRangeRequest{Path: entry.Path, StartLine: entry.StartLine, EndLine: entry.EndLine}
		if !blank(entry.Lines) {
			start, end, err := parseLines(entry.Lines)
			if err != nil {
				return nil, err
			}
			request.StartLine, request.EndLine = start, end
		}
		ranges = append(ranges, request)
	}
	return ranges, nil
}

// parseLines reads "280-400", "280-" (to the end) or "280" (one line).
//
// One string rather than two integers because models drop the second key: in
// benchmark runs an agent sent {"path": "a.go", "startLine": 1195, 1230} five
// times, which is not JSON, so the host rejected each call before Jade saw it.
func parseLines(text string) (int, int, error) {
	text = strings.TrimSpace(text)
	invalid := fmt.Errorf("invalid lines %q: use 280-400, 280- or 280", text)
	from, to, isRange := strings.Cut(text, "-")
	start, err := strconv.Atoi(strings.TrimSpace(from))
	if err != nil || start < 1 {
		return 0, 0, invalid
	}
	if !isRange {
		return start, start, nil
	}
	if strings.TrimSpace(to) == "" {
		return start, 0, nil
	}
	end, err := strconv.Atoi(strings.TrimSpace(to))
	if err != nil || end < start {
		return 0, 0, invalid
	}
	return start, end, nil
}

func blank(value string) bool {
	return strings.TrimSpace(value) == ""
}

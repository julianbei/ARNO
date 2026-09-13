package main

import (
	"encoding/json"
	"fmt"
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
	var ranges []protocol.ReadRangeRequest
	if err := json.Unmarshal(encoded, &ranges); err != nil {
		return nil, fmt.Errorf("invalid ranges: each entry is {path, startLine, endLine}: %w", err)
	}
	return ranges, nil
}

func blank(value string) bool {
	return strings.TrimSpace(value) == ""
}

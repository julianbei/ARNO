package main

import (
	"strings"
	"testing"
)

func TestParseLines(t *testing.T) {
	cases := []struct {
		text       string
		start, end int
		wantErr    bool
	}{
		{"280-400", 280, 400, false},
		{" 280 - 400 ", 280, 400, false},
		{"280-", 280, 0, false},
		{"280", 280, 280, false},
		{"55, 125", 55, 125, false},
		{"55:125", 55, 125, false},
		{"55..125", 55, 125, false},
		{"55 125", 55, 125, false},
		{"400-280", 0, 0, true},
		{"x-10", 0, 0, true},
		{"0-3", 0, 0, true},
	}
	for _, tc := range cases {
		start, end, err := parseLines(tc.text)
		if (err != nil) != tc.wantErr || start != tc.start || end != tc.end {
			t.Errorf("%q: got %d-%d, %v", tc.text, start, end, err)
		}
	}
}

func TestRangesAcceptLines(t *testing.T) {
	ranges, err := rangesArg(map[string]interface{}{"ranges": []interface{}{
		map[string]interface{}{"path": "a.go", "lines": "10-20"},
		map[string]interface{}{"path": "b.go", "startLine": float64(3), "endLine": float64(4)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if ranges[0].StartLine != 10 || ranges[0].EndLine != 20 || ranges[1].StartLine != 3 || ranges[1].EndLine != 4 {
		t.Errorf("got %+v", ranges)
	}
}

// A regex that finds nothing because its parentheses were meant literally is
// answered as literal text.
func TestRegexGrepMissFallsBackToLiteral(t *testing.T) {
	server, root := newTestMCPServer(t)
	writeWorkspaceFile(t, root, "cmd.go", "package cobra\n\nfunc (c *Command) ParseFlags(args []string) error { return nil }\n")

	out, err := callText(t, server, "arno.grep", map[string]interface{}{"query": `func (c \*Command) ParseFlags`, "regex": true})
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if !strings.Contains(out, "searched as literal text") || !strings.Contains(out, "cmd.go:3") {
		t.Errorf("expected a literal fallback match, got:\n%s", out)
	}
}

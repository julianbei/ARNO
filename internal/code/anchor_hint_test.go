package code

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/julianbei/arno/internal/events"
	"github.com/julianbei/arno/internal/protocol"
)

const hintSource = `// DisplayName returns the name shown in help.
func (c *Command) DisplayName() string {
	return c.Name()
}

// UseLine puts out the full usage for a given command (including parents).
func (c *Command) UseLine() string {
	var useline string
	return useline
}
`

func TestAnchorHint(t *testing.T) {
	cases := map[string]struct {
		anchor string
		want   string
	}{
		"one wrong line": {
			anchor: "\treturn c.displayName()\n}\n\n// UseLine puts out the full usage for a given command (including parents).\n",
			want:   `line 3 reads "return c.Name()" where the anchor has "return c.displayName()"`,
		},
		"spaces for tabs": {
			anchor: "func (c *Command) UseLine() string {\n    var useline string\n",
			want:   "the text is at line 7 with different indentation or spacing",
		},
		"nothing like it": {
			anchor: "}\n\nfunc (c *Command) Missing() {\n",
			want:   "no line of the anchor occurs exactly once",
		},
	}
	for name, tc := range cases {
		if got := AnchorHint(hintSource, tc.anchor); !strings.Contains(got, tc.want) {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}

func TestReplaceTextNotFoundCarriesTheHint(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "command.go"), []byte(hintSource), 0o644); err != nil {
		t.Fatal(err)
	}
	index := NewIndex(root, events.NewBus())

	_, err = index.ReplaceTextSource("command.go", "\treturn c.displayName()\n}\n\n// UseLine puts out the full usage for a given command (including parents).", "x")
	if err == nil || !strings.Contains(err.Error(), `line 3 reads "return c.Name()"`) {
		t.Errorf("replace_text: got %v", err)
	}
	_, err = index.InsertSource("command.go", "func (c *Command) useLine() string {", "before", "// x\n")
	if err == nil || !strings.Contains(err.Error(), "anchor text not found in command.go:") {
		t.Errorf("insert: got %v", err)
	}
}

// A grep-style alternation with a literal parenthesis is searched as meant.
func TestGrepStyleAlternationWithAParenthesis(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := "test('lowercase method is uppercased', async t => {});\nconst x = input.toUpperCase();\n"
	if err := os.WriteFile(filepath.Join(root, "main.ts"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	response, err := NewIndex(root, events.NewBus()).Grep(protocol.GrepRequest{Query: `test('lowercase method\|toUpperCase`, Regex: true})
	if err != nil {
		t.Fatalf("expected the retry to succeed, got %v", err)
	}
	if response.Total != 2 {
		t.Errorf("expected both lines, got %+v", response.Matches)
	}
}

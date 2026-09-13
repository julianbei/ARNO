package code

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/julianbei/jade/internal/events"
	"github.com/julianbei/jade/internal/protocol"
)

func TestRegexWithUnbalancedParenSearchesForIt(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := "rgtest!(r3180_look_around_panic, |dir: Dir, mut cmd: TestCommand| {\nrgtest!r3180\n"
	if err := os.WriteFile(filepath.Join(root, "regression.rs"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	response, err := NewIndex(root, events.NewBus()).Grep(protocol.GrepRequest{Query: `rgtest!(r\d+`, Regex: true})
	if err != nil {
		t.Fatalf("an unbalanced paren must be retried as text, got %v", err)
	}
	if response.Total != 1 || response.Matches[0].Line != 1 {
		t.Errorf("expected only the macro call with its parenthesis, got %+v", response.Matches)
	}
	if !strings.Contains(response.Summary, "unbalanced parenthesis") {
		t.Errorf("expected the reading to be stated, got %q", response.Summary)
	}

	if _, err := buildMatcher(`[unclosed`, true, false); err == nil {
		t.Error("a pattern that is invalid for other reasons must still say so")
	}
}

package code

import (
	"strings"
	"testing"

	"github.com/julianbei/jade/internal/protocol"
)

func TestGrepSummaryCarriesProvenance(t *testing.T) {
	complete := grepSummary(protocol.GrepResponse{
		Query: "Store", Total: 3, Files: 2,
		Provenance: protocol.Provenance{Certainty: protocol.CertaintyExact, Source: "text search", Completeness: protocol.CompletenessComplete},
	}, false)
	if complete != "3 matches in 2 files · exact · text search · complete" {
		t.Fatalf("got %q", complete)
	}

	cut := grepSummary(protocol.GrepResponse{
		Query: "Store", Total: 90, Files: 9, Truncated: true, Matches: make([]protocol.GrepMatch, 40),
		Provenance: protocol.Provenance{Certainty: protocol.CertaintyExact, Source: "text search", Completeness: protocol.CompletenessCut},
	}, false)
	if !strings.HasSuffix(cut, " · exact · text search · cut") {
		t.Fatalf("a truncated grep should say cut, got %q", cut)
	}
}
func TestFindProvenance(t *testing.T) {
	cases := []struct {
		textScan     bool
		total, shown int
		want         string
	}{
		{false, 2, 2, "structural · tree-sitter · complete"},
		{true, 2, 2, "text fallback · tree-sitter, text scan · may be incomplete"},
		{false, 9, 5, "structural · tree-sitter · cut"},
	}
	for _, c := range cases {
		if got := findProvenance(c.textScan, c.total, c.shown).String(); got != c.want {
			t.Errorf("findProvenance(%v, %d, %d) = %q, want %q", c.textScan, c.total, c.shown, got, c.want)
		}
	}
}

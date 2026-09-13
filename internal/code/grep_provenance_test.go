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
func TestGlobWithADirectoryReachesBelowIt(t *testing.T) {
	cases := []struct {
		rel, glob string
		want      bool
	}{
		{"state.go", "*.go", true},
		{"internal/core/state.go", "*.go", true},
		{"internal/core/state.go", "internal/core/*", true},
		{"internal/core/state.go", "internal/*", true},
		{"internal/core/state.go", "internal/*.go", true},
		{"internal/core/state.go", "**/*.go", true},
		{"internal/core/state.go", "internal/**/state.go", true},
		{"internal/core/state.ts", "internal/*.go", false},
		{"internal/core/state.go", "internal/code/*", false},
		{"cmd/main.go", "internal/*", false},
	}
	for _, c := range cases {
		if got := pathAllowed(c.rel, c.glob, ""); got != c.want {
			t.Errorf("pathAllowed(%q, %q) = %v, want %v", c.rel, c.glob, got, c.want)
		}
	}
}
func TestNoServerReasonNamesTheKindOfMissing(t *testing.T) {
	index := NewIndex(t.TempDir(), nil)
	if got := index.noServerReason("app.kt"); got != "no language server is known for this file type" {
		t.Fatalf("a file type with no server: got %q", got)
	}
	if got := index.noServerReason("main.go"); got != "language servers are not in use" {
		t.Fatalf("an index without a server manager: got %q", got)
	}
	if got := index.noServerReason("main.go"); strings.Contains(got, "gopls unavailable") {
		t.Fatalf("the old generic wording is back: %q", got)
	}
}
func TestReferenceProvidersAreAskedStrongestFirst(t *testing.T) {
	got := strings.Join(ReferenceProviderIDs(), ", ")
	if got != "language server, gopls, text index" {
		t.Fatalf("references registry order: got %s", got)
	}
	last := referenceProviders()[len(referenceProviders())-1]
	if last.ID() != "text index" {
		t.Fatalf("the always-answering text index must be last, got %s", last.ID())
	}
}

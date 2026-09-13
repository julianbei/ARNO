package render

import (
	"strings"
	"testing"

	"github.com/julianbei/jade/internal/protocol"
)

func TestReferencesLeadWithSummaryAndProvenance(t *testing.T) {
	out := references(protocol.ReferencesResponse{
		Query:      "store.go::Put",
		Source:     "approximate",
		Summary:    "2 approximate references to Put",
		Provenance: protocol.Provenance{Certainty: protocol.CertaintyApproximate, Source: "text index", Completeness: protocol.CompletenessMayBeIncomplete},
		References: []protocol.ReferenceLocation{{Path: "a.go", Symbol: "A"}, {Path: "b.go", Symbol: "B"}},
	})
	first := strings.SplitN(out, "\n", 2)[0]
	if first != "2 approximate references to Put · approximate · text index · may be incomplete" {
		t.Fatalf("first line should be the summary with provenance, got:\n%s", out)
	}
	if strings.Count(out, "2 approximate references to Put") != 1 {
		t.Fatalf("summary printed twice:\n%s", out)
	}
}

func TestNoReferencesCarryProvenance(t *testing.T) {
	out := references(protocol.ReferencesResponse{
		Query:      "store.go::Put",
		Source:     "lsp",
		Provenance: protocol.Provenance{Certainty: protocol.CertaintyExact, Source: "gopls", Completeness: protocol.CompletenessComplete},
	})
	if out != "no references to store.go::Put · exact · gopls · complete" {
		t.Fatalf("got %q", out)
	}
}

func TestOutlineHeaderCarriesParserProvenance(t *testing.T) {
	out := inspect(protocol.InspectResponse{
		Revision: "r3",
		Outline:  []protocol.OutlineItem{{Name: "Store"}},
		Parser:   protocol.ParserInfo{Language: "kotlin", Parser: "heuristic", Note: "no kotlin grammar — declarations were found by a text scan"},
	})
	if first := strings.SplitN(out, "\n", 2)[0]; first != "r3 · text fallback · text scan · may be incomplete" {
		t.Fatalf("got first line %q in:\n%s", first, out)
	}
}
func TestFindFirstLineCarriesProvenance(t *testing.T) {
	provenance := protocol.Provenance{Certainty: protocol.CertaintyStructural, Source: "tree-sitter", Completeness: protocol.CompletenessComplete}
	out := find(protocol.FindResponse{Query: "A", Total: 1, Provenance: provenance, Results: []protocol.FindResult{
		{Path: "a.go", StartLine: 3, EndLine: 5, Kind: "function", Symbol: "A", Body: "func A() {}"},
	}})
	if first := strings.SplitN(out, "\n", 2)[0]; first != "a.go:3-5 function A · structural · tree-sitter · complete" {
		t.Fatalf("got first line %q in:\n%s", first, out)
	}
	none := find(protocol.FindResponse{Query: "B", Summary: `no declarations matching "B"`, Provenance: provenance})
	if none != `no declarations matching "B" · structural · tree-sitter · complete` {
		t.Fatalf("got %q", none)
	}
}

func TestSearchAndRetrievalCarryProvenance(t *testing.T) {
	provenance := protocol.Provenance{Certainty: protocol.CertaintyApproximate, Source: "text index", Completeness: protocol.CompletenessMayBeIncomplete}
	out := search(protocol.SearchResponse{Query: "auth", Provenance: provenance, Hits: []protocol.SearchHit{{Path: "a.go"}}})
	if first := strings.SplitN(out, "\n", 2)[0]; first != `1 hits for "auth" · approximate · text index · may be incomplete` {
		t.Fatalf("got first line %q", first)
	}
	head := strings.SplitN(retrieval(protocol.RetrievalResponse{UsedTokens: 10, MaxTokens: 100, Provenance: provenance}), "\n", 2)[0]
	if head != "10/100 tokens · approximate · text index · may be incomplete" {
		t.Fatalf("got %q", head)
	}
}
func TestRepositoryMapHeadCarriesProvenance(t *testing.T) {
	out := repositoryMap(protocol.RepositoryMapResponse{
		UsedTokens: 90, MaxTokens: 100,
		Included:   []protocol.RepositoryMapItem{{Path: "a.go"}},
		Omitted:    []protocol.RepositoryMapItem{{Path: "b.go"}},
		Provenance: protocol.Provenance{Certainty: protocol.CertaintyApproximate, Source: "text index", Completeness: protocol.CompletenessCut},
	})
	if head := strings.SplitN(out, "\n", 2)[0]; head != "90/100 tokens · 1 included, 1 omitted · approximate · text index · cut" {
		t.Fatalf("got %q", head)
	}
}
func TestContextHeadSaysHowCallersWereFound(t *testing.T) {
	out := contextResponse(protocol.ContextResponse{
		Summary:    "Put method",
		Callers:    []string{"a.go"},
		Provenance: protocol.Provenance{Certainty: protocol.CertaintyApproximate, Source: "text index", Completeness: protocol.CompletenessMayBeIncomplete},
	})
	if head := strings.SplitN(out, "\n", 2)[0]; head != "Put method · callers approximate · text index · may be incomplete" {
		t.Fatalf("got %q", head)
	}
}

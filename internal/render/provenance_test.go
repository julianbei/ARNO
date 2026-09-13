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

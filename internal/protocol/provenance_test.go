package protocol

import "testing"

func TestProvenanceString(t *testing.T) {
	cases := map[string]Provenance{
		"exact · gopls · complete": {Certainty: CertaintyExact, Source: "gopls", Completeness: CompletenessComplete},
		"approximate · text index": {Certainty: CertaintyApproximate, Source: "text index"},
		"":                         {Source: "gopls"},
	}
	for want, provenance := range cases {
		if got := provenance.String(); got != want {
			t.Errorf("%+v rendered %q, want %q", provenance, got, want)
		}
	}
}

func TestParserProvenance(t *testing.T) {
	cases := []struct {
		parser ParserInfo
		want   string
	}{
		{ParserInfo{}, ""},
		{ParserInfo{Language: "go", Parser: "grammar", Complete: true}, "structural · tree-sitter · complete"},
		{ParserInfo{Language: "kotlin", Parser: "heuristic", Note: "no kotlin grammar — declarations were found by a text scan"}, "text fallback · text scan · may be incomplete"},
		{ParserInfo{Language: "go", Parser: "heuristic", Note: "go grammar did not parse this file (often a syntax error)"}, "text fallback · text scan · parse errors"},
	}
	for _, c := range cases {
		if got := ParserProvenance(c.parser).String(); got != c.want {
			t.Errorf("%+v: got %q, want %q", c.parser, got, c.want)
		}
	}
}

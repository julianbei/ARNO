package code

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/julianbei/arno/internal/protocol"
)

func TestSymbolGraphLabelsSameFileEdges(t *testing.T) {
	dir := t.TempDir()
	// Callee declared in the caller's own file — the strongest approximate
	// signal, since no cross-file guessing was needed.
	writeGoFile(t, dir, "local.go", `package fixture

func helper() int {
	return 1
}

func useHelper() int {
	return helper()
}
`)

	graph, err := NewIndex(dir, nil).BuildSymbolGraph()
	if err != nil {
		t.Fatalf("BuildSymbolGraph: %v", err)
	}

	edge := findEdge(t, graph, "local.go::useHelper", "local.go::helper")
	if edge.Confidence != protocol.EdgeSameFile {
		t.Fatalf("expected %q, got %q", protocol.EdgeSameFile, edge.Confidence)
	}
}

func TestSymbolGraphLabelsCrossFileEdgesAsNameMatches(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "target.go", "package fixture\n\nfunc Target() int { return 1 }\n")
	writeGoFile(t, dir, "caller.go", "package fixture\n\nfunc Caller() int { return Target() }\n")

	graph, err := NewIndex(dir, nil).BuildSymbolGraph()
	if err != nil {
		t.Fatalf("BuildSymbolGraph: %v", err)
	}

	edge := findEdge(t, graph, "caller.go::Caller", "target.go::Target")
	// Resolved only because the name is unique repo-wide — a weaker claim
	// than a same-file match, and the edge must say so.
	if edge.Confidence != protocol.EdgeUniqueName {
		t.Fatalf("expected %q, got %q", protocol.EdgeUniqueName, edge.Confidence)
	}
}

func TestSymbolGraphDeclaresItsSourceAndLimitations(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "target.go", "package fixture\n\nfunc Target() int { return 1 }\n")

	graph, err := NewIndex(dir, nil).BuildSymbolGraph()
	if err != nil {
		t.Fatalf("BuildSymbolGraph: %v", err)
	}

	if graph.Source != "approximate" {
		t.Fatalf("expected the graph to declare itself approximate, got %q", graph.Source)
	}
	if len(graph.Limitations) == 0 {
		t.Fatalf("expected the graph to carry its limitations")
	}
	// The caveat must travel with the data, not live only in documentation.
	joined := strings.Join(graph.Limitations, " | ")
	for _, required := range []string{"name", "dynamic dispatch", "cross-language"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("expected limitations to mention %q, got %v", required, graph.Limitations)
		}
	}
}

func TestAmbiguousCalleeIsDroppedNotGuessed(t *testing.T) {
	dir := t.TempDir()
	// Two files declare Target; a call to Target is genuinely ambiguous and
	// must produce no edge at all rather than an arbitrary pick.
	writeGoFile(t, dir, "a.go", "package fixture\n\nfunc Target() int { return 1 }\n")
	writeGoFile(t, dir, "b.go", "package fixture\n\nfunc Target2() int { return 2 }\n")
	writeGoFile(t, dir, "dup.go", "package fixture\n\nfunc Target() int { return 3 }\n")
	writeGoFile(t, dir, "caller.go", "package fixture\n\nfunc Caller() int { return Target() }\n")

	graph, err := NewIndex(dir, nil).BuildSymbolGraph()
	if err != nil {
		t.Fatalf("BuildSymbolGraph: %v", err)
	}

	for _, edge := range graph.Edges {
		if edge.From == "caller.go::Caller" && strings.HasSuffix(edge.To, "::Target") {
			t.Fatalf("expected an ambiguous callee to be dropped, got edge to %s", edge.To)
		}
	}
}

func TestApproximateReferencesCarryEdgeConfidence(t *testing.T) {
	if goplsInstalled() {
		t.Skip("gopls installed — this asserts the approximate path's labeling")
	}

	dir := t.TempDir()
	writeGoFile(t, dir, "target.go", "package fixture\n\nfunc Target() int { return 1 }\n")
	writeGoFile(t, dir, "caller.go", "package fixture\n\nfunc Caller() int { return Target() }\n")

	response, err := NewIndex(dir, nil).References("target.go", "target.go::Target@3")
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	if len(response.References) != 1 {
		t.Fatalf("expected 1 reference, got %+v", response.References)
	}
	if response.References[0].Confidence != protocol.EdgeUniqueName {
		t.Fatalf("expected the per-reference confidence to be propagated, got %q",
			response.References[0].Confidence)
	}
}

func findEdge(t *testing.T, graph protocol.SymbolGraph, from string, to string) protocol.SymbolGraphEdge {
	t.Helper()
	for _, edge := range graph.Edges {
		if edge.From == from && edge.To == to {
			return edge
		}
	}
	t.Fatalf("expected an edge %s -> %s, got %+v", from, to, graph.Edges)
	return protocol.SymbolGraphEdge{}
}

// TestApproximateReferencesNameTheCallingSymbol uses TypeScript on purpose.
// gopls never answers for it, so this exercises the approximate path as a
// real user hits it, rather than skipping wherever gopls happens to be
// installed — which is exactly where the earlier version of this test went
// silent.
func TestApproximateReferencesNameTheCallingSymbol(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFile(t, dir, "target.ts", "export function target(): number {\n\treturn 1;\n}\n")
	// Two distinct callers in ONE file. Without a symbol name they render
	// identically, which is the defect 12.4 fixes — the approximate path has
	// no line numbers to tell them apart.
	writeFixtureFile(t, dir, "caller.ts", `export function first(): number {
	return target();
}

export function second(): number {
	return target();
}
`)

	response, err := NewIndex(dir, nil).References("target.ts", "target.ts::target@1")
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	if response.Source != "approximate" {
		t.Fatalf("expected TypeScript to use the approximate path, got %q", response.Source)
	}
	if len(response.References) != 2 {
		t.Fatalf("expected 2 distinct callers, got %+v", response.References)
	}

	names := map[string]bool{}
	for _, ref := range response.References {
		if ref.Symbol == "" {
			t.Fatalf("expected every approximate reference to name its caller, got %+v", ref)
		}
		names[ref.Symbol] = true
	}
	if !names["first"] || !names["second"] {
		t.Fatalf("expected both callers named, got %v", names)
	}
}

func writeFixtureFile(t *testing.T, dir string, name string, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

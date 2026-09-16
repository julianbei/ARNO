package code

import (
	"fmt"
	"sort"

	sitter "github.com/smacker/go-tree-sitter"
	tspython "github.com/smacker/go-tree-sitter/python"
)

// extractPythonSymbolsTreeSitter parses .py source.
//
// Python had no grammar until now and the text-scan fallback found one
// declaration out of three in a trivial file — it matched `class Store` and
// missed both `def`s. The caveat arno attached ("some may be missing") was
// true but badly understated, which is its own kind of wrong answer.
func extractPythonSymbolsTreeSitter(relPath string, source []byte) ([]Symbol, error) {
	parser := sitter.NewParser()
	parser.SetLanguage(tspython.GetLanguage())
	tree := parser.Parse(nil, source)
	if tree == nil {
		return nil, fmt.Errorf("tree-sitter parse failed")
	}

	out := make([]Symbol, 0)
	walkTree(tree.RootNode(), func(node *sitter.Node) {
		switch node.Type() {
		case "class_definition":
			if name := firstName(node, source); name != "" {
				out = append(out, newSymbol(relPath, "class", name, node))
			}
		case "function_definition":
			name := firstName(node, source)
			if name == "" {
				return
			}
			// A def nested in a class body is a method. Python uses the same
			// node type for both, so the distinction has to come from the
			// enclosing scope rather than the node itself.
			kind := "function"
			if hasAncestorOfType(node, "class_definition") {
				kind = "method"
			}
			out = append(out, newSymbol(relPath, kind, name, node))
		}
	})

	sortSymbols(out)
	return out, nil
}

// hasAncestorOfType reports whether node is nested inside a node of any of the
// given types. Shared by the languages that spell a method as a plain function
// inside a class body: Python, Ruby and Scala all do this.
func hasAncestorOfType(node *sitter.Node, types ...string) bool {
	for parent := node.Parent(); parent != nil; parent = parent.Parent() {
		for _, want := range types {
			if parent.Type() == want {
				return true
			}
		}
	}
	return false
}

// sortSymbols orders declarations by position, then name — the order every
// extractor returns, so an outline reads top-to-bottom like the file does.
func sortSymbols(symbols []Symbol) {
	sort.Slice(symbols, func(a int, b int) bool {
		if symbols[a].From == symbols[b].From {
			return symbols[a].Name < symbols[b].Name
		}
		return symbols[a].From < symbols[b].From
	})
}

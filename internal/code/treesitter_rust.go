package code

import (
	"fmt"
	"sort"

	sitter "github.com/smacker/go-tree-sitter"
	tsrust "github.com/smacker/go-tree-sitter/rust"
)

// extractRustSymbolsTreeSitter parses .rs source.
func extractRustSymbolsTreeSitter(relPath string, source []byte) ([]Symbol, error) {
	parser := sitter.NewParser()
	parser.SetLanguage(tsrust.GetLanguage())
	tree := parser.Parse(nil, source)
	if tree == nil {
		return nil, fmt.Errorf("tree-sitter parse failed")
	}

	out := make([]Symbol, 0)
	walkTree(tree.RootNode(), func(node *sitter.Node) {
		switch node.Type() {
		case "function_item":
			name := firstName(node, source)
			if name == "" {
				return
			}
			kind := "function"
			if insideImplOrTrait(node) {
				kind = "method"
			}
			out = append(out, newSymbol(relPath, kind, name, node))
		case "struct_item":
			if name := firstName(node, source); name != "" {
				out = append(out, newSymbol(relPath, "class", name, node))
			}
		case "enum_item":
			if name := firstName(node, source); name != "" {
				out = append(out, newSymbol(relPath, "type", name, node))
			}
		case "trait_item":
			if name := firstName(node, source); name != "" {
				out = append(out, newSymbol(relPath, "type", name, node))
			}
		}
	})

	sort.Slice(out, func(a int, b int) bool {
		if out[a].From == out[b].From {
			return out[a].Name < out[b].Name
		}
		return out[a].From < out[b].From
	})
	return out, nil
}

// insideImplOrTrait reports whether node is nested inside an impl or trait
// block, distinguishing a method (an associated function) from a free
// top-level function — both use the same "function_item" node type in
// tree-sitter-rust's grammar.
func insideImplOrTrait(node *sitter.Node) bool {
	for parent := node.Parent(); parent != nil; parent = parent.Parent() {
		switch parent.Type() {
		case "impl_item", "trait_item":
			return true
		}
	}
	return false
}

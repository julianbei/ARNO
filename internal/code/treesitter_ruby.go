package code

import (
	"fmt"

	sitter "github.com/smacker/go-tree-sitter"
	tsruby "github.com/smacker/go-tree-sitter/ruby"
)

// extractRubySymbolsTreeSitter parses .rb source.
func extractRubySymbolsTreeSitter(relPath string, source []byte) ([]Symbol, error) {
	parser := sitter.NewParser()
	parser.SetLanguage(tsruby.GetLanguage())
	tree := parser.Parse(nil, source)
	if tree == nil {
		return nil, fmt.Errorf("tree-sitter parse failed")
	}

	out := make([]Symbol, 0)
	walkTree(tree.RootNode(), func(node *sitter.Node) {
		switch node.Type() {
		case "class":
			if name := rubyName(node, source); name != "" {
				out = append(out, newSymbol(relPath, "class", name, node))
			}
		case "module":
			if name := rubyName(node, source); name != "" {
				out = append(out, newSymbol(relPath, "type", name, node))
			}
		case "method":
			name := rubyName(node, source)
			if name == "" {
				return
			}
			kind := "function"
			if hasAncestorOfType(node, "class", "module") {
				kind = "method"
			}
			out = append(out, newSymbol(relPath, kind, name, node))
		case "singleton_method":
			// `def self.create` — a class method. Named separately by the
			// grammar, and worth keeping distinct from an instance method
			// because callers reach it through the class, not an instance.
			if name := rubyName(node, source); name != "" {
				out = append(out, newSymbol(relPath, "method", name, node))
			}
		}
	})

	sortSymbols(out)
	return out, nil
}

// rubyName reads a declaration's name. Ruby names classes and modules with a
// "constant" node rather than an identifier, so firstName's identifier-first
// search would walk past it.
func rubyName(node *sitter.Node, source []byte) string {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "constant", "identifier", "scope_resolution":
			return child.Content(source)
		}
	}
	return ""
}

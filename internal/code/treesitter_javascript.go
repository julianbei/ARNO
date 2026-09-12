package code

import (
	"fmt"

	sitter "github.com/smacker/go-tree-sitter"
	tsjavascript "github.com/smacker/go-tree-sitter/javascript"
)

// extractJavaScriptSymbolsTreeSitter parses .js, .jsx, .mjs and .cjs source.
//
// The text scan already did well here, because JavaScript's declaration syntax
// is close enough to TypeScript's that the heuristic written for one happened
// to fit the other. "Happened to fit" is the problem: it held for `class` and
// `function` and silently stopped at anything else, and nothing in the output
// distinguished a lucky match from a real parse.
func extractJavaScriptSymbolsTreeSitter(relPath string, source []byte) ([]Symbol, error) {
	parser := sitter.NewParser()
	parser.SetLanguage(tsjavascript.GetLanguage())
	tree := parser.Parse(nil, source)
	if tree == nil {
		return nil, fmt.Errorf("tree-sitter parse failed")
	}

	out := make([]Symbol, 0)
	walkTree(tree.RootNode(), func(node *sitter.Node) {
		switch node.Type() {
		case "class_declaration":
			if name := firstName(node, source); name != "" {
				out = append(out, newSymbol(relPath, "class", name, node))
			}
		case "function_declaration", "generator_function_declaration":
			if name := firstName(node, source); name != "" {
				out = append(out, newSymbol(relPath, "function", name, node))
			}
		case "method_definition":
			if name := firstName(node, source); name != "" {
				out = append(out, newSymbol(relPath, "method", name, node))
			}
		case "variable_declarator":
			// `const handler = () => {}` and `const F = function () {}` are
			// declarations in every sense that matters to a caller looking for
			// where something is defined, even though the grammar sees a
			// variable. Only those bound to a function are taken: a plain
			// `const MAX = 10` is data, not a declaration to navigate to.
			name := firstName(node, source)
			if name == "" {
				return
			}
			value := node.ChildByFieldName("value")
			if value == nil {
				return
			}
			switch value.Type() {
			case "arrow_function", "function_expression", "function", "generator_function":
				out = append(out, newSymbol(relPath, "function", name, node))
			}
		}
	})

	sortSymbols(out)
	return out, nil
}

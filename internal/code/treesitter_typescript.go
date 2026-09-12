package code

import (
	"fmt"
	"sort"

	sitter "github.com/smacker/go-tree-sitter"
	tstsx "github.com/smacker/go-tree-sitter/typescript/tsx"
	tstypescript "github.com/smacker/go-tree-sitter/typescript/typescript"
)

// extractTypeScriptSymbolsTreeSitter parses .ts source.
func extractTypeScriptSymbolsTreeSitter(relPath string, source []byte) ([]Symbol, error) {
	return extractTSFamilySymbols(relPath, source, tstypescript.GetLanguage())
}

// extractTSXSymbolsTreeSitter parses .tsx source (JSX-aware superset grammar).
func extractTSXSymbolsTreeSitter(relPath string, source []byte) ([]Symbol, error) {
	return extractTSFamilySymbols(relPath, source, tstsx.GetLanguage())
}

// extractTSFamilySymbols walks a TypeScript or TSX parse tree. Both grammars
// share the same declaration node types, so one walk serves both.
func extractTSFamilySymbols(relPath string, source []byte, lang *sitter.Language) ([]Symbol, error) {
	parser := sitter.NewParser()
	parser.SetLanguage(lang)
	tree := parser.Parse(nil, source)
	if tree == nil {
		return nil, fmt.Errorf("tree-sitter parse failed")
	}

	out := make([]Symbol, 0)
	walkTree(tree.RootNode(), func(node *sitter.Node) {
		switch node.Type() {
		case "function_declaration":
			if name := firstName(node, source); name != "" {
				out = append(out, newSymbol(relPath, "function", name, node))
			}
		case "class_declaration":
			if name := firstName(node, source); name != "" {
				out = append(out, newSymbol(relPath, "class", name, node))
			}
		case "interface_declaration":
			if name := firstName(node, source); name != "" {
				out = append(out, newSymbol(relPath, "type", name, node))
			}
		case "type_alias_declaration":
			if name := firstName(node, source); name != "" {
				out = append(out, newSymbol(relPath, "type", name, node))
			}
		case "enum_declaration":
			if name := firstName(node, source); name != "" {
				out = append(out, newSymbol(relPath, "type", name, node))
			}
		case "method_definition":
			if name := firstName(node, source); name != "" {
				out = append(out, newSymbol(relPath, "method", name, node))
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

package code

import (
	"fmt"

	sitter "github.com/smacker/go-tree-sitter"
	tsscala "github.com/smacker/go-tree-sitter/scala"
)

// extractScalaSymbolsTreeSitter parses .scala source.
func extractScalaSymbolsTreeSitter(relPath string, source []byte) ([]Symbol, error) {
	parser := sitter.NewParser()
	parser.SetLanguage(tsscala.GetLanguage())
	tree := parser.Parse(nil, source)
	if tree == nil {
		return nil, fmt.Errorf("tree-sitter parse failed")
	}

	out := make([]Symbol, 0)
	walkTree(tree.RootNode(), func(node *sitter.Node) {
		switch node.Type() {
		case "class_definition", "case_class_definition":
			if name := scalaName(node, source); name != "" {
				out = append(out, newSymbol(relPath, "class", name, node))
			}
		case "object_definition", "trait_definition", "type_definition", "enum_definition":
			if name := scalaName(node, source); name != "" {
				out = append(out, newSymbol(relPath, "type", name, node))
			}
		case "function_definition":
			name := scalaName(node, source)
			if name == "" {
				return
			}
			// Scala defs live inside a template body for classes, objects and
			// traits alike, so anything enclosed is a method and only a
			// top-level def is a free function.
			kind := "function"
			if hasAncestorOfType(node, "class_definition", "case_class_definition",
				"object_definition", "trait_definition", "enum_definition") {
				kind = "method"
			}
			out = append(out, newSymbol(relPath, kind, name, node))
		}
	})

	sortSymbols(out)
	return out, nil
}

func scalaName(node *sitter.Node, source []byte) string {
	if field := node.ChildByFieldName("name"); field != nil {
		return field.Content(source)
	}
	return firstName(node, source)
}

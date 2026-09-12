package code

import (
	"fmt"

	sitter "github.com/smacker/go-tree-sitter"
	tsjava "github.com/smacker/go-tree-sitter/java"
)

// extractJavaSymbolsTreeSitter parses .java source.
//
// Java was the worst case of the text-scan fallback: it found *nothing* in a
// file with a class and a method, and reported that empty outline with a note
// saying some declarations "may be missing". A caveat that understates a total
// miss reads as a small caveat, so the fallback was worse than an honest
// refusal would have been.
func extractJavaSymbolsTreeSitter(relPath string, source []byte) ([]Symbol, error) {
	parser := sitter.NewParser()
	parser.SetLanguage(tsjava.GetLanguage())
	tree := parser.Parse(nil, source)
	if tree == nil {
		return nil, fmt.Errorf("tree-sitter parse failed")
	}

	out := make([]Symbol, 0)
	walkTree(tree.RootNode(), func(node *sitter.Node) {
		switch node.Type() {
		case "class_declaration", "record_declaration":
			if name := javaName(node, source); name != "" {
				out = append(out, newSymbol(relPath, "class", name, node))
			}
		case "interface_declaration", "enum_declaration", "annotation_type_declaration":
			if name := javaName(node, source); name != "" {
				out = append(out, newSymbol(relPath, "type", name, node))
			}
		case "method_declaration", "constructor_declaration":
			if name := javaName(node, source); name != "" {
				out = append(out, newSymbol(relPath, "method", name, node))
			}
		}
	})

	sortSymbols(out)
	return out, nil
}

// javaName reads a declaration's name, skipping the modifiers and type nodes
// that precede it. Every Java declaration node carries its name in a "name"
// field, which is more reliable than positional search: `public void put` has
// a type node before the identifier, so taking the first identifier child
// would return the return type on some shapes.
func javaName(node *sitter.Node, source []byte) string {
	if field := node.ChildByFieldName("name"); field != nil {
		return field.Content(source)
	}
	return firstName(node, source)
}

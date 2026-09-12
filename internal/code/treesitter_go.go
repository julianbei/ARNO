package code

import (
	"fmt"
	"sort"

	sitter "github.com/smacker/go-tree-sitter"
	tsgolang "github.com/smacker/go-tree-sitter/golang"
)

func extractGoSymbolsTreeSitter(relPath string, source []byte) ([]Symbol, error) {
	parser := sitter.NewParser()
	parser.SetLanguage(tsgolang.GetLanguage())
	tree := parser.Parse(nil, source)
	if tree == nil {
		return nil, fmt.Errorf("tree-sitter parse failed")
	}

	out := make([]Symbol, 0)
	walkTree(tree.RootNode(), func(node *sitter.Node) {
		nodeType := node.Type()
		switch nodeType {
		case "function_declaration":
			name := firstName(node, source)
			if name == "" {
				return
			}
			out = append(out, newSymbol(relPath, "function", name, node))
		case "method_declaration":
			name := firstName(node, source)
			if name == "" {
				return
			}
			out = append(out, newSymbol(relPath, "method", name, node))
		case "type_spec":
			name := firstName(node, source)
			if name == "" {
				return
			}
			kind := "type"
			if hasDescendantType(node, "struct_type") {
				kind = "class"
			}
			out = append(out, newSymbol(relPath, kind, name, node))
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

func walkTree(node *sitter.Node, visit func(*sitter.Node)) {
	if node == nil {
		return
	}
	visit(node)
	for i := 0; i < int(node.ChildCount()); i++ {
		walkTree(node.Child(i), visit)
	}
}

func newSymbol(relPath string, kind string, name string, node *sitter.Node) Symbol {
	from := int(node.StartPoint().Row) + 1
	to := int(node.EndPoint().Row) + 1
	return Symbol{
		ID:   fmt.Sprintf("%s::%s@%d", relPath, name, from),
		Kind: kind,
		Name: name,
		Path: relPath,
		From: from,
		To:   to,
	}
}

func firstName(node *sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		t := child.Type()
		if t == "identifier" || t == "field_identifier" || t == "type_identifier" || t == "property_identifier" {
			return child.Content(source)
		}
	}

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		name := firstName(child, source)
		if name != "" {
			return name
		}
	}
	return ""
}

func hasDescendantType(node *sitter.Node, want string) bool {
	found := false
	walkTree(node, func(n *sitter.Node) {
		if found || n == nil {
			return
		}
		if n.Type() == want {
			found = true
		}
	})
	return found
}

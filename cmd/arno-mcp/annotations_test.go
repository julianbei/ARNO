package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// Hosts act on MCP annotations (auto-approving reads, warning before
// deletions), and TDQS grades a description against them, so every tool
// needs an honest entry and no entry may outlive its tool.
func TestEveryToolIsAnnotated(t *testing.T) {
	served := map[string]bool{}
	for _, tool := range tools() {
		served[tool.Name] = true
		if tool.Annotations == nil {
			t.Errorf("%s has no entry in toolAnnotationsByName", tool.Name)
			continue
		}
		hints := tool.Annotations
		if hints.ReadOnlyHint == nil && hints.DestructiveHint == nil {
			t.Errorf("%s has an empty annotations object", tool.Name)
		}
		if hints.ReadOnlyHint != nil && *hints.ReadOnlyHint && hints.DestructiveHint != nil && *hints.DestructiveHint {
			t.Errorf("%s is marked both read-only and destructive", tool.Name)
		}
	}
	for name := range toolAnnotationsByName {
		if !served[name] {
			t.Errorf("toolAnnotationsByName has %s, which the catalog does not serve", name)
		}
	}
}

func TestAnnotationsSerializeExplicitFalse(t *testing.T) {
	for _, tool := range listedTools("core") {
		if tool.Name != "arno.insert" {
			continue
		}
		data, err := json.Marshal(tool)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), `"annotations":{"destructiveHint":false}`) {
			t.Fatalf("insert should say it destroys nothing, got %s", data)
		}
		return
	}
	t.Fatal("arno.insert is not in the core profile")
}

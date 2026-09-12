package code

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractTypeScriptSymbolsTreeSitterFindsDeclarations(t *testing.T) {
	source := []byte(`
interface Greeter {
	greet(): string;
}

type GreetResult = string;

class SessionManager implements Greeter {
	greet(): string {
		return "hi";
	}
}

function standalone(): number {
	return 1;
}
`)

	symbols, err := extractTypeScriptSymbolsTreeSitter("session.ts", source)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	byName := map[string]Symbol{}
	for _, symbol := range symbols {
		byName[symbol.Name] = symbol
	}

	if byName["Greeter"].Kind != "type" {
		t.Fatalf("expected Greeter interface as kind=type, got %#v", byName["Greeter"])
	}
	if byName["GreetResult"].Kind != "type" {
		t.Fatalf("expected GreetResult type alias as kind=type, got %#v", byName["GreetResult"])
	}
	if byName["SessionManager"].Kind != "class" {
		t.Fatalf("expected SessionManager as kind=class, got %#v", byName["SessionManager"])
	}
	if byName["greet"].Kind != "method" {
		t.Fatalf("expected greet as kind=method, got %#v", byName["greet"])
	}
	if byName["standalone"].Kind != "function" {
		t.Fatalf("expected standalone as kind=function, got %#v", byName["standalone"])
	}
}

func TestOutlineUsesTreeSitterForTypeScriptFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "session.ts")
	source := "class SessionManager {\n\trefresh(): void {}\n}\n"
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	idx := NewIndex(root, nil)
	symbols, err := idx.Outline("session.ts")
	if err != nil {
		t.Fatalf("outline: %v", err)
	}

	found := false
	for _, symbol := range symbols {
		if symbol.Name == "SessionManager" && symbol.Kind == "class" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected SessionManager class via tree-sitter path, got %#v", symbols)
	}
}

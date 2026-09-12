package code

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractRustSymbolsTreeSitterFindsDeclarations(t *testing.T) {
	source := []byte(`
struct Session {
	token: String,
}

enum SessionState {
	Active,
	Expired,
}

trait Greeter {
	fn greet(&self) -> String;
}

impl Session {
	fn refresh(&self) -> String {
		"ok".to_string()
	}
}

fn standalone() -> i32 {
	1
}
`)

	symbols, err := extractRustSymbolsTreeSitter("session.rs", source)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	byName := map[string]Symbol{}
	for _, symbol := range symbols {
		byName[symbol.Name] = symbol
	}

	if byName["Session"].Kind != "class" {
		t.Fatalf("expected Session struct as kind=class, got %#v", byName["Session"])
	}
	if byName["SessionState"].Kind != "type" {
		t.Fatalf("expected SessionState enum as kind=type, got %#v", byName["SessionState"])
	}
	if byName["Greeter"].Kind != "type" {
		t.Fatalf("expected Greeter trait as kind=type, got %#v", byName["Greeter"])
	}
	if byName["refresh"].Kind != "method" {
		t.Fatalf("expected refresh (inside impl) as kind=method, got %#v", byName["refresh"])
	}
	if byName["standalone"].Kind != "function" {
		t.Fatalf("expected standalone (top-level fn) as kind=function, got %#v", byName["standalone"])
	}
}

func TestOutlineUsesTreeSitterForRustFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "session.rs")
	source := "struct Session {}\n\nimpl Session {\n\tfn refresh(&self) {}\n}\n"
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	idx := NewIndex(root, nil)
	symbols, err := idx.Outline("session.rs")
	if err != nil {
		t.Fatalf("outline: %v", err)
	}

	found := false
	for _, symbol := range symbols {
		if symbol.Name == "Session" && symbol.Kind == "class" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected Session struct via tree-sitter path, got %#v", symbols)
	}
}

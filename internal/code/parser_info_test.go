package code

import (
	"strings"
	"testing"
)

func TestParserForReportsGrammarsAsComplete(t *testing.T) {
	for path, want := range map[string]string{
		"a.go":         "go",
		"a.ts":         "typescript",
		"a.tsx":        "tsx",
		"src/lib.rs":   "rust",
		"nested/x.GO":  "go",
		"deep/dir/a.p": "",
	} {
		info := ParserFor(path, "tree-sitter")
		if !info.Complete {
			t.Fatalf("%s: expected a grammar parse to be complete", path)
		}
		if info.Note != "" {
			t.Fatalf("%s: expected no caveat on a grammar parse, got %q", path, info.Note)
		}
		if info.Language != want {
			t.Fatalf("%s: expected language %q, got %q", path, want, info.Language)
		}
	}
}

func TestParserForNamesTheLanguageItCannotParse(t *testing.T) {
	// The live 11.2 case: a Python file whose outline silently omitted a
	// function. Naming the language beats "this file" because the reader can
	// judge how much to distrust the result.
	info := ParserFor("hello.py", "regex")
	if info.Complete {
		t.Fatalf("expected the heuristic to be reported as incomplete")
	}
	if info.Language != "python" {
		t.Fatalf("expected python, got %q", info.Language)
	}
	if !strings.Contains(info.Note, "python") {
		t.Fatalf("expected the note to name the language, got %q", info.Note)
	}
	if !strings.Contains(info.Note, "missing") {
		t.Fatalf("expected the note to say declarations may be missing, got %q", info.Note)
	}
}

func TestParserForDistinguishesAFailedGrammarFromAnAbsentOne(t *testing.T) {
	// A .go file that fell back to the heuristic did not do so because Go is
	// unsupported — it is almost always a syntax error, and telling the caller
	// "no go grammar" would send them looking for the wrong thing entirely.
	info := ParserFor("broken.go", "regex")
	if info.Complete {
		t.Fatalf("expected incomplete")
	}
	if !strings.Contains(info.Note, "syntax error") {
		t.Fatalf("expected the note to suggest a syntax error, got %q", info.Note)
	}
	if strings.Contains(info.Note, "no go grammar") {
		t.Fatalf("expected it not to claim Go is unsupported, got %q", info.Note)
	}
}

func TestParserForHandlesAnUnknownExtension(t *testing.T) {
	info := ParserFor("data.wat", "regex")
	if info.Complete {
		t.Fatalf("expected incomplete")
	}
	if info.Language != "" {
		t.Fatalf("expected no language guess, got %q", info.Language)
	}
	if !strings.Contains(info.Note, "text scan") {
		t.Fatalf("expected the note to say how symbols were found, got %q", info.Note)
	}
}

func TestOutlineStructuredReportsItsParser(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "a.go", "package fixture\n\nfunc Present() int {\n\treturn 1\n}\n")
	writeGoFile(t, dir, "hello.py", "def greet(name):\n    return name\n\nclass Greeter:\n    pass\n")

	index := NewIndex(dir, nil)

	_, _, goParser, err := index.OutlineStructured("a.go")
	if err != nil {
		t.Fatalf("OutlineStructured(go): %v", err)
	}
	if !goParser.Complete || goParser.Language != "go" {
		t.Fatalf("expected a complete go parse, got %+v", goParser)
	}

	_, _, pyParser, err := index.OutlineStructured("hello.py")
	if err != nil {
		t.Fatalf("OutlineStructured(py): %v", err)
	}
	// Python gained a real grammar, so this is now the complete case. The
	// heuristic assertion below moved to a language that still has no grammar.
	if !pyParser.Complete {
		t.Fatalf("expected a complete python parse, got %+v", pyParser)
	}
	if pyParser.Language != "python" {
		t.Fatalf("expected python named, got %+v", pyParser)
	}
}

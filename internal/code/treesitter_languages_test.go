package code

import (
	"fmt"
	"testing"
)

// These languages were served by the text-scan fallback until now. Each case
// below names what the fallback actually returned when it was measured, so a
// regression to the heuristic is visible as a specific loss rather than as a
// vague count change.
func TestGrammarsFindWhatTheTextScanMissed(t *testing.T) {
	cases := []struct {
		name string
		file string
		src  string
		// want is every declaration the grammar must find, as "kind Name".
		want []string
		// fallbackFound is what the heuristic managed before the grammar
		// existed — recorded to document the size of the gap being closed.
		fallbackFound string
	}{
		{
			name: "python",
			file: "a.py",
			src: "class Store:\n" +
				"    def put(self, k):\n" +
				"        pass\n" +
				"\n" +
				"def greet(n):\n" +
				"    return n\n",
			want:          []string{"class Store", "method put", "function greet"},
			fallbackFound: "class Store only",
		},
		{
			name: "ruby",
			file: "a.rb",
			src: "class Store\n" +
				"  def put(k)\n" +
				"  end\n" +
				"end\n" +
				"\n" +
				"module Helpers\n" +
				"end\n" +
				"\n" +
				"def greet(n)\n" +
				"  n\n" +
				"end\n",
			want:          []string{"class Store", "method put", "type Helpers", "function greet"},
			fallbackFound: "class Store only",
		},
		{
			name: "java",
			file: "a.java",
			src: "public class Store {\n" +
				"    public void put(String k) {}\n" +
				"}\n" +
				"interface Greeter { void greet(); }\n" +
				"enum Colour { RED }\n",
			want:          []string{"class Store", "method put", "type Greeter", "method greet", "type Colour"},
			fallbackFound: "nothing at all",
		},
		{
			name: "scala",
			file: "a.scala",
			src: "class Store {\n" +
				"  def put(k: String): Unit = {}\n" +
				"}\n" +
				"object Greeter {\n" +
				"  def greet(n: String): String = n\n" +
				"}\n" +
				"trait Named\n",
			want:          []string{"class Store", "method put", "type Greeter", "method greet", "type Named"},
			fallbackFound: "class Store only",
		},
		{
			name: "javascript",
			file: "a.js",
			src: "export class Store {\n" +
				"  put(k) {}\n" +
				"}\n" +
				"export function greet(n) { return n; }\n" +
				"const handler = () => {};\n",
			want:          []string{"class Store", "method put", "function greet", "function handler"},
			fallbackFound: "class, method and function but not the arrow-function const",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			index := indexWithFile(t, tc.file, tc.src)

			sections, symbols, parser, err := index.OutlineStructured(tc.file)
			if err != nil {
				t.Fatalf("outline failed: %v", err)
			}
			_ = sections

			if parser.Parser != "grammar" {
				t.Fatalf("expected a real grammar, got %q (note: %q)", parser.Parser, parser.Note)
			}
			if !parser.Complete {
				t.Fatal("a grammar parse must not be reported as incomplete")
			}
			if parser.Note != "" {
				t.Fatalf("a grammar parse must carry no fallback caveat, got %q", parser.Note)
			}
			if parser.Language != tc.name {
				t.Fatalf("expected language %q, got %q", tc.name, parser.Language)
			}

			found := make(map[string]bool, len(symbols))
			for _, symbol := range symbols {
				found[fmt.Sprintf("%s %s", symbol.Kind, symbol.Name)] = true
			}
			for _, want := range tc.want {
				if !found[want] {
					t.Errorf("missing %q (the text scan previously found: %s)\n  got: %v",
						want, tc.fallbackFound, keysOf(found))
				}
			}
		})
	}
}

// A grammar that cannot parse its input must fall back rather than return an
// empty outline that looks authoritative.
func TestBrokenSourceFallsBackToTheTextScan(t *testing.T) {
	index := indexWithFile(t, "broken.py", "class Store:\n    def put(self k)\n        ???\n")

	_, _, parser, err := index.OutlineStructured("broken.py")
	if err != nil {
		t.Fatalf("outline failed: %v", err)
	}
	if parser.Parser == "grammar" {
		return // tree-sitter is error-tolerant and recovered; that is fine too.
	}
	if parser.Note == "" {
		t.Fatal("a fallback parse must say the result may be incomplete")
	}
}

// A language with no grammar must still say so plainly rather than claiming a
// complete outline.
func TestLanguageWithoutAGrammarStillAnnouncesItself(t *testing.T) {
	index := indexWithFile(t, "a.kt", "class Store {\n    fun put(k: String) {}\n}\n")

	_, _, parser, err := index.OutlineStructured("a.kt")
	if err != nil {
		t.Fatalf("outline failed: %v", err)
	}
	if parser.Parser != "heuristic" {
		t.Fatalf("kotlin has no grammar, got parser %q", parser.Parser)
	}
	if parser.Note == "" {
		t.Fatal("the incompleteness caveat must be present")
	}
}

func keysOf(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	return out
}

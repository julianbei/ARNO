package lsp

import "testing"

// LSP counts columns in UTF-16 code units, jade counts bytes. The two agree
// for ASCII and diverge for everything else, which is why getting this wrong
// survives casual testing and then answers about the wrong identifier in any
// file containing an accent or an emoji.
func TestUTF16ColumnConversion(t *testing.T) {
	cases := []struct {
		name string
		line string
		// byteColumn is 1-based, as jade reports positions.
		byteCol int
		want    int
	}{
		{name: "ascii start", line: "func Greet()", byteCol: 1, want: 0},
		{name: "ascii middle", line: "func Greet()", byteCol: 6, want: 5},

		// "é" is two bytes and one UTF-16 unit, so every column after it is
		// one lower in LSP's counting than in jade's.
		{name: "after a two-byte rune", line: "// café Greet", byteCol: 10, want: 8},

		// An emoji outside the BMP is four bytes and *two* UTF-16 units — a
		// surrogate pair — so it is not simply "runes instead of bytes"
		// either. A rune-counting implementation is wrong here.
		{name: "after a surrogate pair", line: "// 🚀 Greet", byteCol: 8, want: 5},

		{name: "past the end is clamped", line: "abc", byteCol: 99, want: 3},
		{name: "zero is treated as start", line: "abc", byteCol: 0, want: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := utf16Column(tc.line, tc.byteCol); got != tc.want {
				t.Fatalf("utf16Column(%q, %d) = %d, want %d", tc.line, tc.byteCol, got, tc.want)
			}
		})
	}
}

// Positions make the round trip, so a server's answer maps back onto the same
// byte the question was asked about.
func TestColumnConversionRoundTrips(t *testing.T) {
	lines := []string{
		"func Greet() {}",
		"// café au lait",
		"// 🚀 launch(x)",
		"日本語のコメント xyz",
	}
	for _, line := range lines {
		for byteCol := 1; byteCol <= len(line)+1; byteCol++ {
			// Only byte offsets that start a rune are meaningful positions.
			if byteCol-1 < len(line) && !startsRune(line, byteCol-1) {
				continue
			}
			units := utf16Column(line, byteCol)
			if back := byteColumn(line, units); back != byteCol {
				t.Fatalf("round trip failed for %q at byte %d: units=%d back=%d", line, byteCol, units, back)
			}
		}
	}
}

func startsRune(s string, index int) bool {
	return s[index]&0xC0 != 0x80
}

func TestLanguageForPath(t *testing.T) {
	cases := map[string]string{
		"a.go":      "go",
		"a.ts":      "typescript",
		"a.tsx":     "tsx",
		"a.js":      "javascript",
		"a.mjs":     "javascript",
		"a.rs":      "rust",
		"a.py":      "python",
		"a.rb":      "ruby",
		"a.java":    "java",
		"a.scala":   "scala",
		"a.kt":      "",
		"README.md": "",
		"dir/a.py":  "python",
		"UPPER.PY":  "python",
	}
	for path, want := range cases {
		if got := LanguageForPath(path); got != want {
			t.Errorf("LanguageForPath(%q) = %q, want %q", path, got, want)
		}
	}
}

// Every language jade parses with a grammar should have a server configured,
// or semantic features are permanently unavailable for it with no indication
// that anyone decided so.
func TestEveryGrammarLanguageHasAServerSpec(t *testing.T) {
	for _, language := range []string{
		"go", "typescript", "tsx", "javascript", "rust",
		"python", "ruby", "java", "scala",
	} {
		if _, ok := SpecFor(language); !ok {
			t.Errorf("no language server configured for %q", language)
		}
	}
}

// A spec must name a language ID: servers use it to pick their analysis, and
// an empty one is quietly accepted by some and rejected by others.
func TestEverySpecDeclaresALanguageID(t *testing.T) {
	for _, language := range Languages() {
		spec, _ := SpecFor(language)
		if spec.LanguageID == "" {
			t.Errorf("%s has no LanguageID", language)
		}
		if spec.Command == "" {
			t.Errorf("%s has no Command", language)
		}
	}
}

// Resolve must carry the alternative's own arguments. solargraph needs
// "stdio" and ruby-lsp does not, so reusing the primary's args launches the
// fallback wrongly — it starts, says nothing, and times out.
func TestResolveUsesTheAlternativesOwnArguments(t *testing.T) {
	spec := ServerSpec{
		Language: "ruby",
		Command:  "definitely-not-installed-primary",
		Args:     []string{"--primary-only"},
		Alternatives: []AlternativeSpec{
			{Command: "sh", Args: []string{"-c", "true"}},
		},
	}

	resolved, ok := spec.Resolve()
	if !ok {
		t.Fatal("sh should be found as an alternative")
	}
	if resolved.Command != "sh" {
		t.Fatalf("expected the alternative command, got %q", resolved.Command)
	}
	if len(resolved.Args) != 2 || resolved.Args[0] != "-c" {
		t.Fatalf("expected the alternative's own args, got %v", resolved.Args)
	}
}

package code

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The reported case: a caller who knows the name should not have to learn a
// line number first. A bare "path::Name" now resolves when it is unambiguous,
// which removes the outline/read_symbol round trip that preceded every edit.
func TestReadSymbolResolvesABareNameWhenItIsUnique(t *testing.T) {
	index := indexWithFile(t, "greet.go", `package main

func Greet(name string) string {
	return "hello " + name
}
`)

	symbol, body, err := index.ReadSymbol("greet.go", "greet.go::Greet", 0)
	if err != nil {
		t.Fatalf("a unique bare name should resolve, got: %v", err)
	}
	if symbol.ID != "greet.go::Greet@3" {
		t.Fatalf("expected the resolved ID, got %q", symbol.ID)
	}
	if !strings.Contains(body, "hello ") {
		t.Fatalf("expected the real body, got %q", body)
	}
}

// A stale ID — the symbol moved since the caller last looked — resolves the
// same way, since the name still identifies exactly one declaration.
func TestReadSymbolResolvesAStaleLineNumber(t *testing.T) {
	index := indexWithFile(t, "greet.go", `package main

func Greet(name string) string {
	return "hello " + name
}
`)

	symbol, _, err := index.ReadSymbol("greet.go", "greet.go::Greet@99", 0)
	if err != nil {
		t.Fatalf("a stale line should still resolve by name, got: %v", err)
	}
	if symbol.ID != "greet.go::Greet@3" {
		t.Fatalf("expected the current ID, got %q", symbol.ID)
	}
}

// Two declarations sharing a name must produce both candidates rather than a
// single arbitrary pick — the same promise the ambiguous-name path makes.
func TestNotFoundListsEveryCandidateWhenTheNameIsAmbiguous(t *testing.T) {
	index := indexWithFile(t, "store.go", `package main

type A struct{}
type B struct{}

func (a A) Put() {}

func (b B) Put() {}
`)

	_, _, err := index.ReadSymbol("store.go", "store.go::Put", 0)
	if err == nil {
		t.Fatal("expected an error for an ambiguous bare name")
	}
	message := err.Error()
	if !strings.Contains(message, "did you mean one of:") {
		t.Fatalf("error should offer the candidates, got: %v", message)
	}
	for _, want := range []string{"store.go::Put@6", "store.go::Put@8"} {
		if !strings.Contains(message, want) {
			t.Fatalf("error should include %s, got: %v", want, message)
		}
	}
}

// A name that genuinely is not in the file must not acquire an invented
// suggestion — an approximation that announces itself is the point, and a
// confident wrong hint is worse than none.
func TestNotFoundStaysPlainWhenNothingMatches(t *testing.T) {
	index := indexWithFile(t, "greet.go", `package main

func Greet(name string) string {
	return "hello " + name
}
`)

	_, _, err := index.ReadSymbol("greet.go", "greet.go::Farewell", 0)
	if err == nil {
		t.Fatal("expected an error for a name that is not present")
	}
	if strings.Contains(err.Error(), "did you mean") {
		t.Fatalf("no candidate exists, so nothing should be suggested, got: %v", err)
	}
}

// ReplaceSymbolSource is the tool the report actually hit, so it gets its own
// check rather than relying on the shared helper being wired everywhere.
func TestReplaceSymbolResolvesABareName(t *testing.T) {
	index := indexWithFile(t, "greet.go", `package main

func Greet(name string) string {
	return "hello " + name
}
`)

	symbol, _, err := index.ReplaceSymbolSource("greet.go::Greet", "func Greet() string { return \"hi\" }")
	if err != nil {
		t.Fatalf("a unique bare name should resolve, got: %v", err)
	}
	if symbol.ID != "greet.go::Greet@3" {
		t.Fatalf("expected the resolved ID, got %q", symbol.ID)
	}
}

func TestSymbolIDName(t *testing.T) {
	cases := []struct {
		id   string
		want string
		ok   bool
	}{
		{"greet.go::Greet@3", "Greet", true},
		{"greet.go::Greet", "Greet", true},
		{"a/b/greet.go::Greet@12", "Greet", true},
		{"Greet", "", false},
		{"greet.go::", "", false},
	}
	for _, tc := range cases {
		got, ok := symbolIDName(tc.id)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("symbolIDName(%q) = (%q, %v), want (%q, %v)", tc.id, got, ok, tc.want, tc.ok)
		}
	}
}

func indexWithFile(t *testing.T, name string, content string) *Index {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return NewIndex(root, nil)
}

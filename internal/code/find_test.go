package code

import (
	"strings"
	"testing"
)

func findFixture(t *testing.T) *Index {
	t.Helper()
	dir := t.TempDir()
	writeGoFile(t, dir, "a.go", `package fixture

func Apply() int {
	return 1
}

func ApplyRequest() int {
	return 2
}

type Applier struct{}
`)
	writeGoFile(t, dir, "b.go", `package fixture

func Unrelated() int {
	return 3
}
`)
	return NewIndex(dir, nil)
}

func TestFindReturnsBodyNotJustLocation(t *testing.T) {
	// The whole point: one call answers "show me this", where outline+
	// read_symbol took two.
	response, err := findFixture(t).FindSymbols("Unrelated", "", 0, 0)
	if err != nil {
		t.Fatalf("FindSymbols: %v", err)
	}
	if len(response.Results) != 1 {
		t.Fatalf("expected 1 result, got %+v", response.Results)
	}
	result := response.Results[0]
	if !strings.Contains(result.Body, "return 3") {
		t.Fatalf("expected the declaration body, got %q", result.Body)
	}
	if result.Path != "b.go" || result.StartLine != 3 {
		t.Fatalf("expected a precise location, got %+v", result)
	}
}

func TestFindPrefersExactMatchesExclusively(t *testing.T) {
	// Asking for "Apply" must not bury it under ApplyRequest and Applier.
	response, err := findFixture(t).FindSymbols("Apply", "", 0, 0)
	if err != nil {
		t.Fatalf("FindSymbols: %v", err)
	}
	if len(response.Results) != 1 {
		t.Fatalf("expected only the exact match, got %+v", response.Results)
	}
	if response.Results[0].Symbol != "Apply" {
		t.Fatalf("expected Apply, got %q", response.Results[0].Symbol)
	}
}

func TestFindFallsBackToSubstringWhenNoExactMatch(t *testing.T) {
	response, err := findFixture(t).FindSymbols("Appli", "", 0, 0)
	if err != nil {
		t.Fatalf("FindSymbols: %v", err)
	}
	if len(response.Results) == 0 {
		t.Fatalf("expected substring matches when nothing matches exactly")
	}
	for _, result := range response.Results {
		if !strings.Contains(strings.ToLower(result.Symbol), "appli") {
			t.Fatalf("unexpected match %q", result.Symbol)
		}
	}
}

func TestFindFiltersByKind(t *testing.T) {
	// ARNO reports a Go struct as "class" and a func as "function", which no
	// caller would guess — every natural spelling must reach the same set.
	for _, spelling := range []string{"type", "struct", "class"} {
		response, err := findFixture(t).FindSymbols("Appli", spelling, 0, 0)
		if err != nil {
			t.Fatalf("FindSymbols(%q): %v", spelling, err)
		}
		if len(response.Results) != 1 || response.Results[0].Symbol != "Applier" {
			t.Fatalf("kind %q: expected only the type, got %+v", spelling, response.Results)
		}
	}

	// Note "Appl" not "Appli": "apply" does not contain "appli".
	for _, spelling := range []string{"func", "function"} {
		response, err := findFixture(t).FindSymbols("Appl", spelling, 0, 0)
		if err != nil {
			t.Fatalf("FindSymbols(%q): %v", spelling, err)
		}
		if len(response.Results) != 2 {
			t.Fatalf("kind %q: expected both funcs, got %+v", spelling, response.Results)
		}
		for _, result := range response.Results {
			if result.Kind == "class" {
				t.Fatalf("kind %q: expected the type excluded, got %+v", spelling, result)
			}
		}
	}
}

func TestFindReportsTrueTotalWhenCapped(t *testing.T) {
	response, err := findFixture(t).FindSymbols("Appl", "", 1, 0)
	if err != nil {
		t.Fatalf("FindSymbols: %v", err)
	}
	if len(response.Results) != 1 {
		t.Fatalf("expected the limit honoured, got %d", len(response.Results))
	}
	// A cap must cost detail, never accuracy.
	if response.Total < 2 {
		t.Fatalf("expected the true total reported, got %d", response.Total)
	}
}

func TestFindTruncatesLongBodiesAndSaysSo(t *testing.T) {
	dir := t.TempDir()
	var body strings.Builder
	body.WriteString("package fixture\n\nfunc Long() int {\n")
	for i := 0; i < 60; i++ {
		body.WriteString("\t_ = 1\n")
	}
	body.WriteString("\treturn 1\n}\n")
	writeGoFile(t, dir, "long.go", body.String())

	response, err := NewIndex(dir, nil).FindSymbols("Long", "", 0, 5)
	if err != nil {
		t.Fatalf("FindSymbols: %v", err)
	}
	if len(response.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(response.Results))
	}
	if !response.Results[0].Truncated {
		t.Fatalf("expected truncation to be reported")
	}
	if lines := strings.Count(response.Results[0].Body, "\n") + 1; lines > 5 {
		t.Fatalf("expected at most 5 lines, got %d", lines)
	}
}

func TestFindReportsNoMatchesClearly(t *testing.T) {
	response, err := findFixture(t).FindSymbols("NothingLikeThis", "", 0, 0)
	if err != nil {
		t.Fatalf("FindSymbols: %v", err)
	}
	if len(response.Results) != 0 || response.Total != 0 {
		t.Fatalf("expected no matches, got %+v", response)
	}
	if !strings.Contains(response.Summary, "no declarations") {
		t.Fatalf("expected a clear summary, got %q", response.Summary)
	}
}

func TestFindRejectsEmptyQuery(t *testing.T) {
	if _, err := findFixture(t).FindSymbols("  ", "", 0, 0); err == nil {
		t.Fatalf("expected an empty query to be rejected")
	}
}

func TestFindIsStableAcrossCalls(t *testing.T) {
	// Unstable ordering makes a response undiffable and a re-read look like
	// a change.
	index := findFixture(t)
	first, _ := index.FindSymbols("Appl", "", 0, 0)
	second, _ := index.FindSymbols("Appl", "", 0, 0)

	if len(first.Results) != len(second.Results) {
		t.Fatalf("result count changed between calls")
	}
	for i := range first.Results {
		if first.Results[i].SymbolID != second.Results[i].SymbolID {
			t.Fatalf("ordering changed between calls at %d", i)
		}
	}
}

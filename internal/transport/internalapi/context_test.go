package internalapi

import (
	"strings"
	"testing"
)

func TestNormalizePurposeFallsBackToTheWidestSet(t *testing.T) {
	// An unrecognized purpose must not silently drop sections: omitting one
	// the caller needed is worse than sending one they did not.
	for _, input := range []string{"", "whatever", "REFACTOR", "  "} {
		if got := normalizePurpose(input); got != "modify" {
			t.Fatalf("normalizePurpose(%q) = %q, want modify", input, got)
		}
	}
}

func TestNormalizePurposeAcceptsSynonyms(t *testing.T) {
	cases := map[string]string{
		"understand": "understand",
		"read":       "understand",
		"explain":    "understand",
		"debug":      "debug",
		"fix":        "debug",
		"test":       "test",
		"Modify":     "modify",
	}
	for input, want := range cases {
		if got := normalizePurpose(input); got != want {
			t.Fatalf("normalizePurpose(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestPurposeNarrowsSections(t *testing.T) {
	// "modify" is the widest set; every other purpose is a strict subset.
	sections := []string{"callers", "tests", "types", "diagnostics", "changes"}
	for _, section := range sections {
		if !purposeWants("modify", section) {
			t.Fatalf("modify should include %s", section)
		}
	}

	if purposeWants("understand", "diagnostics") {
		t.Fatalf("reading code does not need diagnostics")
	}
	if purposeWants("debug", "types") {
		t.Fatalf("debugging does not need the type tour")
	}
	if !purposeWants("test", "tests") {
		t.Fatalf("the test purpose must include existing tests")
	}
	if purposeWants("test", "diagnostics") {
		t.Fatalf("test purpose should not pull diagnostics")
	}
}

func TestIsTestFileRecognizesGoAndTypeScriptConventions(t *testing.T) {
	testFiles := []string{
		"internal/code/index_test.go",
		"src/app.test.ts",
		"src/app.spec.ts",
		"pkg/thing_test.go",
	}
	for _, path := range testFiles {
		if !isTestFile(path) {
			t.Fatalf("expected %s to be recognized as a test file", path)
		}
	}

	productionFiles := []string{
		"internal/code/index.go",
		"src/app.ts",
		"testdata/fixture.go",
	}
	for _, path := range productionFiles {
		if isTestFile(path) {
			t.Fatalf("expected %s not to be treated as a test file", path)
		}
	}
}

func TestCapStringsReportsTrueTotal(t *testing.T) {
	values := []string{"a", "b", "c", "d", "e"}

	capped, total := capStrings(values, 3)
	if len(capped) != 3 {
		t.Fatalf("expected 3 values, got %d", len(capped))
	}
	// A cap costs detail, never accuracy.
	if total != 5 {
		t.Fatalf("expected the true total 5, got %d", total)
	}

	capped, total = capStrings(values, 10)
	if len(capped) != 5 || total != 5 {
		t.Fatalf("expected an under-cap list to pass through, got %d/%d", len(capped), total)
	}
}

func TestSortedKeysIsDeterministic(t *testing.T) {
	set := map[string]bool{"c": true, "a": true, "b": true}
	got := sortedKeys(set)
	if strings.Join(got, ",") != "a,b,c" {
		t.Fatalf("expected sorted output, got %v", got)
	}
}

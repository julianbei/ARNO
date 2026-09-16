package jobs

import (
	"testing"
)

// DetectCommandCandidates finds npm scripts and Makefile targets no one has
// declared, ranks recognisable operational names first, and caps the list.
// "build" and "test" are deliberately left out of this fixture: they are
// names check itself would discover and run, and TestDetectCommandCandidatesExcludesCheckCoveredScripts
// covers that exclusion on its own — mixing the two here would make this
// test depend on that behaviour without saying so.
func TestDetectCommandCandidatesFromNpmScripts(t *testing.T) {
	hasNPM(t)
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{
  "name": "demo",
  "scripts": {
    "test:e2e": "playwright test",
    "lint": "eslint .",
    "dev": "vite",
    "weirdo": "echo hi"
  }
}`, 0o644)

	candidates := DetectCommandCandidates(dir)
	if len(candidates) == 0 {
		t.Fatal("expected candidates from package.json scripts")
	}
	byName := map[string]string{}
	for _, c := range candidates {
		byName[c.Name] = c.Run
	}
	for name, run := range map[string]string{
		"test:e2e": "npm run test:e2e", "lint": "npm run lint", "dev": "npm run dev", "weirdo": "npm run weirdo",
	} {
		if byName[name] != run {
			t.Errorf("expected %s -> %q, got %q (all: %+v)", name, run, byName[name], candidates)
		}
	}
	// A priority name (test:e2e, then lint) before the unranked ones.
	if candidates[0].Name != "test:e2e" {
		t.Errorf("expected the priority name first, got %+v", candidates)
	}
	if _, ok := byName["weirdo"]; !ok {
		t.Errorf("expected an unrecognised script still listed under the cap, got %+v", candidates)
	}
}

// An npm script never present is never a candidate: detection parses what
// `npm run` itself reports, not package.json directly.
func TestDetectCommandCandidatesOnlyRealScripts(t *testing.T) {
	hasNPM(t)
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"name": "demo", "scripts": {"serve": "echo ok"}}`, 0o644)
	candidates := DetectCommandCandidates(dir)
	if len(candidates) != 1 || candidates[0].Name != "serve" {
		t.Fatalf("expected exactly the one declared script, got %+v", candidates)
	}
}

func TestDetectCommandCandidatesFromMakefile(t *testing.T) {
	hasMake(t)
	dir := t.TempDir()
	// "docs" and "clean", not "build" or "test": those are names check's own
	// Makefile discovery would pick up too, which is the separate exclusion
	// TestDetectCommandCandidatesExcludesCheckCoveredScripts covers.
	writeFile(t, dir, "Makefile", ".PHONY: docs clean\n\ndocs:\n\techo building docs\n\nclean:\n\trm -rf dist\n\n.DEFAULT_GOAL := docs\n", 0o644)

	candidates := DetectCommandCandidates(dir)
	byName := map[string]string{}
	for _, c := range candidates {
		byName[c.Name] = c.Run
	}
	if byName["docs"] != "make docs" || byName["clean"] != "make clean" {
		t.Fatalf("expected docs and clean as make targets, got %+v", candidates)
	}
	if _, ok := byName[".PHONY"]; ok {
		t.Fatalf(".PHONY leaked into candidates: %+v", candidates)
	}
	if _, ok := byName[".DEFAULT_GOAL"]; ok {
		t.Fatalf("a dot-prefixed make directive leaked into candidates: %+v", candidates)
	}
}

// A script check already runs (build, typecheck, tests) is not offered again
// as a candidate; one it does not cover still is.
func TestDetectCommandCandidatesExcludesCheckCoveredScripts(t *testing.T) {
	hasNPM(t)
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{
  "name": "demo",
  "scripts": {
    "build": "tsc -b",
    "test": "vitest run",
    "debug": "vitest --inspect"
  }
}`, 0o644)

	candidates := DetectCommandCandidates(dir)
	names := map[string]bool{}
	for _, c := range candidates {
		names[c.Name] = true
	}
	if names["build"] || names["test"] {
		t.Fatalf("expected build and test left out as already check-covered, got %+v", candidates)
	}
	if !names["debug"] {
		t.Fatalf("expected debug, which check does not cover, still offered, got %+v", candidates)
	}
}

// Without package.json or a Makefile there is nothing to detect.
func TestDetectCommandCandidatesEmptyWithoutManifests(t *testing.T) {
	dir := t.TempDir()
	if candidates := DetectCommandCandidates(dir); len(candidates) != 0 {
		t.Fatalf("expected no candidates in a manifest-free directory, got %+v", candidates)
	}
}

// More candidates than the cap: priority names win the slots, and the list
// never exceeds maxCommandCandidates.
func TestDetectCommandCandidatesCapsAndPrioritises(t *testing.T) {
	hasNPM(t)
	dir := t.TempDir()
	scripts := map[string]string{}
	for i := 0; i < 15; i++ {
		scripts[string(rune('a'+i))+"-script"] = "echo " + string(rune('a'+i))
	}
	scripts["lint"] = "eslint ."
	scripts["dev"] = "vite"
	writeManifest(t, dir, scripts)

	candidates := DetectCommandCandidates(dir)
	if len(candidates) != maxCommandCandidates {
		t.Fatalf("expected exactly %d candidates, got %d: %+v", maxCommandCandidates, len(candidates), candidates)
	}
	names := map[string]bool{}
	for _, c := range candidates {
		names[c.Name] = true
	}
	if !names["lint"] || !names["dev"] {
		t.Fatalf("expected the priority names to survive the cap, got %+v", candidates)
	}
}

func writeManifest(t *testing.T, dir string, scripts map[string]string) {
	t.Helper()
	b := `{"name": "demo", "scripts": {`
	first := true
	for name, run := range scripts {
		if !first {
			b += ","
		}
		first = false
		b += `"` + name + `": "` + run + `"`
	}
	b += "}}"
	writeFile(t, dir, "package.json", b, 0o644)
}

package jobs

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func hasNPM(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm not available in this environment")
	}
}

func hasCargo(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Skip("cargo not available in this environment")
	}
}

func writePackageJSON(t *testing.T, dir string, scripts string) {
	t.Helper()
	content := `{"name":"fixture","version":"1.0.0","scripts":{` + scripts + `}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
}

func TestDiscoverCommandFindsRealNPMScript(t *testing.T) {
	hasNPM(t)
	dir := t.TempDir()
	writePackageJSON(t, dir, `"test":"echo ran-tests","build":"echo ran-build"`)

	name, args, ok := discoverCommand(dir, "tests")
	if !ok {
		t.Fatalf("expected tests to resolve for an npm project")
	}
	if name != "npm" || len(args) != 2 || args[0] != "run" || args[1] != "test" {
		t.Fatalf("expected [npm run test], got %q %v", name, args)
	}
}

func TestDiscoverCommandPrefersFirstMatchingNPMCandidate(t *testing.T) {
	hasNPM(t)
	dir := t.TempDir()
	// Both "test" and "test:unit" exist; "test" is first in the candidate
	// list, so it should win.
	writePackageJSON(t, dir, `"test:unit":"echo unit","test":"echo all"`)

	_, args, ok := discoverCommand(dir, "tests")
	if !ok {
		t.Fatalf("expected tests to resolve")
	}
	if args[1] != "test" {
		t.Fatalf("expected the first candidate (test) to win, got %v", args)
	}
}

func TestDiscoverCommandSkipsNPMWhenNoMatchingScriptExists(t *testing.T) {
	hasNPM(t)
	dir := t.TempDir()
	// A real package.json, but with no script matching any "tests"
	// candidate — discovery must not invent one, and falls through to the
	// Go default rather than returning a script npm would reject.
	writePackageJSON(t, dir, `"start":"echo start"`)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module probe\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	name, _, ok := discoverCommand(dir, "tests")
	if !ok {
		t.Fatalf("expected a fallback command to resolve")
	}
	if name == "npm" {
		t.Fatalf("expected npm to be skipped when no candidate script exists")
	}
}

func TestNPMScriptForUsesRealNPMOutputNotPackageJSONParsing(t *testing.T) {
	hasNPM(t)
	dir := t.TempDir()
	writePackageJSON(t, dir, `"typecheck":"echo tsc"`)

	script, ok := npmScriptFor(dir, "typecheck")
	if !ok {
		t.Fatalf("expected typecheck script to be discovered")
	}
	if script != "typecheck" {
		t.Fatalf("expected typecheck, got %q", script)
	}
}

func TestDiscoverCommandFindsCargoForRustProject(t *testing.T) {
	hasCargo(t)
	dir := t.TempDir()

	manifest := "[package]\nname = \"fixture\"\nversion = \"0.1.0\"\nedition = \"2021\"\n"
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write Cargo.toml: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "lib.rs"), []byte("pub fn noop() {}\n"), 0o644); err != nil {
		t.Fatalf("write lib.rs: %v", err)
	}

	name, args, ok := discoverCommand(dir, "tests")
	if !ok {
		t.Fatalf("expected tests to resolve for a cargo project")
	}
	if name != "cargo" {
		t.Fatalf("expected the cargo command, got %q %v", name, args)
	}
	if len(args) < 2 || args[0] != "test" || args[1] != "--workspace" {
		t.Fatalf("expected [test --workspace], got %v", args)
	}
}

func TestCargoArgsForRejectsNonCargoDirectory(t *testing.T) {
	hasCargo(t)
	dir := t.TempDir()
	// No Cargo.toml at all, so `cargo metadata` fails — the manifest
	// verification step must reject rather than returning args anyway.
	if _, ok := cargoArgsFor(dir, "tests"); ok {
		t.Fatalf("expected cargo discovery to fail without a real cargo project")
	}
}

func TestDiscoverCommandStillPrefersMakefileOverEcosystem(t *testing.T) {
	hasMake(t)
	hasNPM(t)
	dir := t.TempDir()

	writePackageJSON(t, dir, `"test":"echo npm-tests"`)
	makefile := "test:\n\t@echo makefile-tests\n"
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(makefile), 0o644); err != nil {
		t.Fatalf("write Makefile: %v", err)
	}

	name, args, ok := discoverCommand(dir, "tests")
	if !ok {
		t.Fatalf("expected tests to resolve")
	}
	if name != "make" {
		t.Fatalf("expected a repository Makefile target to win over the npm script, got %q %v", name, args)
	}
}

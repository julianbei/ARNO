package jobs

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func hasMake(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make not available in this environment")
	}
}

func TestDiscoverCommandFallsBackToGoSubcommandWithoutMakefile(t *testing.T) {
	dir := t.TempDir()
	writeGoMod(t, dir)

	name, args, ok := discoverCommand(dir, "typecheck")
	if !ok {
		t.Fatalf("expected typecheck to resolve")
	}
	if name != "go" {
		t.Fatalf("expected fallback to the go command, got %q %v", name, args)
	}
}

func TestDiscoverCommandReturnsFalseForUnknownKind(t *testing.T) {
	dir := t.TempDir()

	_, _, ok := discoverCommand(dir, "mystery")
	if ok {
		t.Fatalf("expected unknown kind to resolve as not-ok")
	}
}

func TestDiscoverCommandPrefersVerifiedMakefileTarget(t *testing.T) {
	hasMake(t)
	dir := t.TempDir()

	makefile := "vet:\n\t@echo custom-vet-target-ran\n"
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(makefile), 0o644); err != nil {
		t.Fatalf("write Makefile: %v", err)
	}

	name, args, ok := discoverCommand(dir, "typecheck")
	if !ok {
		t.Fatalf("expected typecheck to resolve")
	}
	if name != "make" || len(args) != 1 || args[0] != "vet" {
		t.Fatalf("expected discovery to prefer the verified Makefile target, got %q %v", name, args)
	}
}

func TestDiscoverCommandIgnoresUndeclaredMakefileTargets(t *testing.T) {
	hasMake(t)
	dir := t.TempDir()
	writeGoMod(t, dir)

	// Makefile exists but declares no target matching any candidate for
	// "tests" (only "vet" is declared) — should fall back to go test.
	makefile := "vet:\n\t@echo vet-only\n"
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(makefile), 0o644); err != nil {
		t.Fatalf("write Makefile: %v", err)
	}

	name, args, ok := discoverCommand(dir, "tests")
	if !ok {
		t.Fatalf("expected tests to resolve")
	}
	if name != "go" {
		t.Fatalf("expected fallback to go test since no matching Makefile target exists, got %q %v", name, args)
	}
}

func TestRunValidationCommandActuallyInvokesTheMakefileTarget(t *testing.T) {
	hasMake(t)
	dir := t.TempDir()

	makefile := "build:\n\t@echo makefile-build-target-ran\n"
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(makefile), 0o644); err != nil {
		t.Fatalf("write Makefile: %v", err)
	}

	r := NewRunner(nil)
	id := r.Start("build")
	r.RunValidationCommand(id, dir, "build")

	status, _ := waitForCompletion(t, r, id)
	if status != "completed" {
		t.Fatalf("expected status completed, got %s", status)
	}

	output, ok := r.Output(id)
	if !ok {
		t.Fatalf("expected job output to be present")
	}
	if !strings.Contains(output.Raw, "makefile-build-target-ran") {
		t.Fatalf("expected the real Makefile target to have run, got %q", output.Raw)
	}
}

// writeGoMod marks a directory as a Go project. The Go default is no longer a
// catch-all for anything unrecognised: it applies to Go projects, so a test
// that wants it has to say so.
func writeGoMod(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module probe\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
}

// A project arno cannot identify must say so rather than running Go commands
// in it. `go build ./...` in a Python repository fails for a reason that has
// nothing to do with the code, which sends the reader after the wrong problem.
func TestDiscoverCommandRefusesToGuessGoForANonGoProject(t *testing.T) {
	dir := t.TempDir()

	if _, _, ok := discoverCommand(dir, "build"); ok {
		t.Fatal("an unidentifiable project must not resolve to the Go default")
	}
}

func TestDiscoverCommandFindsEcosystemsByManifest(t *testing.T) {
	cases := []struct {
		manifest string
		kind     string
		want     string
	}{
		{"pom.xml", "tests", "mvn"},
		{"build.sbt", "tests", "sbt"},
		{"build.gradle", "tests", "gradle"},
		{"build.gradle.kts", "build", "gradle"},
		{"pyproject.toml", "tests", "python3"},
		{"setup.py", "tests", "python3"},
		{"Gemfile", "tests", "bundle"},
	}
	for _, tc := range cases {
		t.Run(tc.manifest, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, tc.manifest), []byte("\n"), 0o644); err != nil {
				t.Fatalf("write %s: %v", tc.manifest, err)
			}
			name, args, ok := discoverCommand(dir, tc.kind)
			if !ok {
				t.Fatalf("%s should resolve %s", tc.manifest, tc.kind)
			}
			if name != tc.want {
				t.Fatalf("expected %q, got %q %v", tc.want, name, args)
			}
		})
	}
}

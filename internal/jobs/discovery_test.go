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

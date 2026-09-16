package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMissingRegistryIsEmptyNotAnError(t *testing.T) {
	// Most repos will never declare commands, and ARNO has to work in one
	// that has not opted in.
	registry, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("expected a missing registry to load empty, got %v", err)
	}
	if len(registry.Names()) != 0 {
		t.Fatalf("expected no commands, got %v", registry.Names())
	}
}

func TestLoadMalformedRegistryFails(t *testing.T) {
	// The opposite of the case above, and the reason it is not symmetric:
	// treating an unparseable file as empty would report "no command build"
	// for a command the caller can see declared in the file.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, Dir), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, RelPath), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := Load(root); err == nil {
		t.Fatalf("expected a malformed registry to fail loudly")
	}
}

func TestDeclarePersistsAcrossLoads(t *testing.T) {
	root := t.TempDir()
	registry, _ := Load(root)

	if _, err := registry.Declare("vet", Command{Run: "go vet ./...", Description: "static analysis"}); err != nil {
		t.Fatalf("Declare: %v", err)
	}

	reloaded, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	command, ok := reloaded.Lookup("vet")
	if !ok {
		t.Fatalf("expected vet to survive a reload, got %v", reloaded.Names())
	}
	if command.Run != "go vet ./..." {
		t.Fatalf("expected the run string preserved, got %q", command.Run)
	}
	if command.Description != "static analysis" {
		t.Fatalf("expected the description preserved, got %q", command.Description)
	}
}

func TestDeclareReportsReplacement(t *testing.T) {
	// Silently overwriting a command someone else declared should be visible
	// in the response, not only in the git diff.
	root := t.TempDir()
	registry, _ := Load(root)

	if replaced, _ := registry.Declare("build", Command{Run: "go build ./..."}); replaced {
		t.Fatalf("expected the first declaration not to report a replacement")
	}
	replaced, err := registry.Declare("build", Command{Run: "make build"})
	if err != nil {
		t.Fatalf("Declare: %v", err)
	}
	if !replaced {
		t.Fatalf("expected the second declaration to report a replacement")
	}
	if command, _ := registry.Lookup("build"); command.Run != "make build" {
		t.Fatalf("expected the new run string to win, got %q", command.Run)
	}
}

func TestNamesAreSortedAndStable(t *testing.T) {
	root := t.TempDir()
	registry, _ := Load(root)
	for _, name := range []string{"test", "build", "lint"} {
		if _, err := registry.Declare(name, Command{Run: "true"}); err != nil {
			t.Fatalf("Declare(%q): %v", name, err)
		}
	}

	first := strings.Join(registry.Names(), ",")
	if first != "build,lint,test" {
		t.Fatalf("expected sorted names, got %q", first)
	}
	if second := strings.Join(registry.Names(), ","); second != first {
		t.Fatalf("expected a stable order, got %q then %q", first, second)
	}
}

func TestDeclareRejectsBadNames(t *testing.T) {
	registry, _ := Load(t.TempDir())
	for _, name := range []string{"", "-leading", "Upper", "has space", "rm -rf /", strings.Repeat("x", 41)} {
		if _, err := registry.Declare(name, Command{Run: "true"}); err == nil {
			t.Fatalf("expected name %q to be rejected", name)
		}
	}
}

func TestDeclareRejectsEmptyRun(t *testing.T) {
	registry, _ := Load(t.TempDir())
	if _, err := registry.Declare("noop", Command{Run: "   "}); err == nil {
		t.Fatalf("expected an empty run string to be rejected")
	}
}

func TestDeclareRejectsAnEntireScript(t *testing.T) {
	// A command long enough to be a script has smuggled the thing past the
	// review the registry exists to enable.
	registry, _ := Load(t.TempDir())
	_, err := registry.Declare("huge", Command{Run: strings.Repeat("echo hi; ", 500)})
	if err == nil {
		t.Fatalf("expected an over-long run string to be rejected")
	}
	if !strings.Contains(err.Error(), "script") {
		t.Fatalf("expected the error to suggest a script file, got %v", err)
	}
}

func TestRegistryIsBounded(t *testing.T) {
	registry, _ := Load(t.TempDir())
	for i := 0; i < maxCommands; i++ {
		if _, err := registry.Declare(padName(i), Command{Run: "true"}); err != nil {
			t.Fatalf("Declare %d: %v", i, err)
		}
	}
	if _, err := registry.Declare("onemore", Command{Run: "true"}); err == nil {
		t.Fatalf("expected the registry to be bounded at %d", maxCommands)
	}
	// Redeclaring an existing command must still work at the limit, otherwise
	// a full registry becomes uneditable.
	if _, err := registry.Declare(padName(0), Command{Run: "false"}); err != nil {
		t.Fatalf("expected redeclaration to work at the limit, got %v", err)
	}
}

func padName(i int) string {
	return "cmd" + string(rune('a'+i/26)) + string(rune('a'+i%26))
}

func TestRemoveIsIdempotent(t *testing.T) {
	root := t.TempDir()
	registry, _ := Load(root)
	if _, err := registry.Declare("gone", Command{Run: "true"}); err != nil {
		t.Fatalf("Declare: %v", err)
	}

	existed, err := registry.Remove("gone")
	if err != nil || !existed {
		t.Fatalf("expected the command removed, got existed=%v err=%v", existed, err)
	}
	// Removing something already absent satisfies the caller's intent.
	existed, err = registry.Remove("gone")
	if err != nil {
		t.Fatalf("expected a second remove to succeed, got %v", err)
	}
	if existed {
		t.Fatalf("expected the second remove to report nothing was there")
	}

	reloaded, _ := Load(root)
	if _, ok := reloaded.Lookup("gone"); ok {
		t.Fatalf("expected the removal to persist")
	}
}

func TestUnknownCommandErrorListsWhatIsAvailable(t *testing.T) {
	// The teachable error is the whole reason a name beats a shell string:
	// the list usually contains the thing the caller meant.
	err := UnknownCommandError("lnit", []string{"build", "lint", "test"})
	if !strings.Contains(err.Error(), "lint") {
		t.Fatalf("expected the available commands listed, got %v", err)
	}
}

func TestUnknownCommandErrorSaysHowToStartWhenNoneDeclared(t *testing.T) {
	err := UnknownCommandError("lint", nil)
	if !strings.Contains(err.Error(), RelPath) {
		t.Fatalf("expected the error to name the registry file, got %v", err)
	}
}

func TestRegistryFileIsHumanReadable(t *testing.T) {
	// The file is meant to be reviewed in a diff. A single-line blob would
	// make every change look like a full rewrite.
	root := t.TempDir()
	registry, _ := Load(root)
	if _, err := registry.Declare("lint", Command{Run: "golangci-lint run"}); err != nil {
		t.Fatalf("Declare: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, RelPath))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), "\n  ") {
		t.Fatalf("expected indented JSON, got:\n%s", data)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Fatalf("expected a trailing newline")
	}
}

package internalapi

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/julianbei/arno/internal/code"
	"github.com/julianbei/arno/internal/protocol"
)

func hasNPM(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm not available in this environment")
	}
}

func writePackageJSON(t *testing.T, root string, scripts string) {
	t.Helper()
	content := `{"name": "demo", "scripts": {` + scripts + `}}`
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// An unknown run_command name, in a repository whose manifest names a
// plausible one, points at it instead of leaving the caller to guess again —
// the failure class the 2026-09-16 benchmark measured directly. "debug" and
// "lint", not "build" or "test": those are names check's own discovery would
// resolve and run, a separate exclusion internal/jobs covers on its own.
func TestRunCommandUnknownNameShowsDetectedCandidates(t *testing.T) {
	hasNPM(t)
	server, root := commandServer(t)
	writePackageJSON(t, root, `"debug": "vitest --inspect", "lint": "eslint ."`)

	_, err := server.RunCommand(protocol.RunCommandRequest{Name: "test-unit"})
	if err == nil {
		t.Fatal("expected an error for an undeclared name")
	}
	if !strings.Contains(err.Error(), `no command "test-unit"`) {
		t.Fatalf("expected the usual unknown-command message, got %v", err)
	}
	if !strings.Contains(err.Error(), "Detected candidates:") || !strings.Contains(err.Error(), "lint") || !strings.Contains(err.Error(), "npm run lint") {
		t.Fatalf("expected detected candidates in the refusal, got %v", err)
	}
}

// A candidate already declared is not repeated as a suggestion — it is
// answered, not proposed.
func TestRunCommandCandidatesExcludeAlreadyDeclared(t *testing.T) {
	hasNPM(t)
	server, root := commandServer(t)
	writePackageJSON(t, root, `"debug": "vitest --inspect", "lint": "eslint ."`)
	if _, err := server.DeclareCommand(protocol.DeclareCommandRequest{Name: "debug", Run: "npm run debug"}); err != nil {
		t.Fatalf("DeclareCommand: %v", err)
	}

	_, err := server.RunCommand(protocol.RunCommandRequest{Name: "typo"})
	if err == nil || strings.Contains(err.Error(), "  debug") {
		t.Fatalf("expected the already-declared 'debug' left out of candidates, got %v", err)
	}
	if !strings.Contains(err.Error(), "lint") {
		t.Fatalf("expected the undeclared 'lint' still offered, got %v", err)
	}
}

// The empty-name listing carries candidates too, not only when nothing is
// declared: a repository with one declared command may still have more.
func TestRunCommandEmptyNameListsCandidatesAlongsideDeclared(t *testing.T) {
	hasNPM(t)
	server, root := commandServer(t)
	writePackageJSON(t, root, `"smoke": "vitest run smoke", "debug": "vitest --inspect", "lint": "eslint ."`)
	if _, err := server.DeclareCommand(protocol.DeclareCommandRequest{Name: "smoke-test", Run: "npm run smoke"}); err != nil {
		t.Fatalf("DeclareCommand: %v", err)
	}

	response, err := server.RunCommand(protocol.RunCommandRequest{})
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	if len(response.Available) != 1 || response.Available[0].Name != "smoke-test" {
		t.Fatalf("expected the one declared command, got %+v", response.Available)
	}
	names := map[string]bool{}
	for _, c := range response.Candidates {
		names[c.Name] = true
	}
	if !names["debug"] || !names["lint"] {
		t.Fatalf("expected undeclared candidates listed, got %+v", response.Candidates)
	}
}

// With no declared commands and no detectable manifest, there is nothing to
// suggest — the original teachable error, unchanged.
func TestRunCommandUnknownNameWithoutManifestStaysPlain(t *testing.T) {
	server, _ := commandServer(t)
	_, err := server.RunCommand(protocol.RunCommandRequest{Name: "anything"})
	if err == nil || strings.Contains(err.Error(), "Detected candidates:") {
		t.Fatalf("expected no candidates block without a manifest, got %v", err)
	}
}

// capabilities() reports detected-but-undeclared commands the same way
// run_command does, so an agent that calls it first — the tool the server
// instructions tell it to call before learning from failed calls — already
// sees the vocabulary instead of discovering it through a refusal.
func TestCapabilitiesReportsUndeclaredCommandCandidates(t *testing.T) {
	hasNPM(t)
	server, root := commandServer(t)
	server.index = code.NewIndex(root, nil)
	writePackageJSON(t, root, `"debug": "vitest --inspect", "lint": "eslint ."`)
	if _, err := server.DeclareCommand(protocol.DeclareCommandRequest{Name: "debug-run", Run: "npm run debug"}); err != nil {
		t.Fatalf("DeclareCommand: %v", err)
	}

	response, err := server.Capabilities()
	if err != nil {
		t.Fatalf("Capabilities: %v", err)
	}
	names := map[string]bool{}
	for _, c := range response.CommandCandidates {
		names[c.Name] = true
	}
	if !names["debug"] || !names["lint"] {
		t.Fatalf("expected the detected npm scripts among candidates, got %+v", response.CommandCandidates)
	}
}

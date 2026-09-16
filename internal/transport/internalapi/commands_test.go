package internalapi

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/julianbei/arno/internal/code"
	"github.com/julianbei/arno/internal/commands"
	"github.com/julianbei/arno/internal/jobs"
	"github.com/julianbei/arno/internal/protocol"
	"github.com/julianbei/arno/internal/workspace"
)

// commandServer builds the smallest Server that can run a declared command.
// Only the workspace manager and the job runner are involved in this path, so
// the rest stay nil rather than being faked: a nil that panics on use is a
// better test than a stub that silently accepts a call this code should never
// make.
func commandServer(t *testing.T) (*Server, string) {
	t.Helper()
	root := t.TempDir()
	return &Server{
		workspace: workspace.NewManager(root, nil),
		jobs:      jobs.NewRunner(nil),
	}, root
}

func TestRunCommandWithNoNameListsInsteadOfErroring(t *testing.T) {
	// An agent that does not know this repo's vocabulary must be able to ask
	// with the tool it already has, rather than guessing a name to trigger the
	// teachable error or falling back to reading the registry file by hand.
	server, _ := commandServer(t)

	if _, err := server.DeclareCommand(protocol.DeclareCommandRequest{
		Name: "hello", Run: "echo hi", Description: "greets",
	}); err != nil {
		t.Fatalf("DeclareCommand: %v", err)
	}

	response, err := server.RunCommand(protocol.RunCommandRequest{})
	if err != nil {
		t.Fatalf("expected listing, got error %v", err)
	}
	if response.Status != "listed" {
		t.Fatalf("expected a listing, got %+v", response)
	}
	if len(response.Available) != 1 || response.Available[0].Name != "hello" {
		t.Fatalf("expected the declared command listed, got %+v", response.Available)
	}
	if response.Available[0].Description != "greets" {
		t.Fatalf("expected the description carried through, got %+v", response.Available[0])
	}
}

func TestDeclareThenRunRoundTrip(t *testing.T) {
	server, root := commandServer(t)

	declared, err := server.DeclareCommand(protocol.DeclareCommandRequest{
		Name: "touchit", Run: "echo made > made.txt",
	})
	if err != nil {
		t.Fatalf("DeclareCommand: %v", err)
	}
	if declared.Replaced {
		t.Fatalf("expected a first declaration not to report a replacement")
	}
	if declared.Path != commands.RelPath {
		t.Fatalf("expected the registry path reported, got %q", declared.Path)
	}

	response, err := server.RunCommand(protocol.RunCommandRequest{Name: "touchit", Wait: true})
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	if !response.Passed {
		t.Fatalf("expected the command to pass, got %+v", response)
	}
	if response.Run != "echo made > made.txt" {
		t.Fatalf("expected the run string echoed back, got %q", response.Run)
	}

	// The verdict is not enough on its own — check the command actually ran
	// in the workspace root.
	if _, err := os.Stat(filepath.Join(root, "made.txt")); err != nil {
		t.Fatalf("expected the command to run in the workspace root: %v", err)
	}
}

func TestRunCommandUsesAShellSoPipesAndAndsWork(t *testing.T) {
	// Splitting the run string into argv instead of handing it to a shell
	// would make every real project command — which have pipes and `&&` —
	// undeclarable, sending the caller back to bash.
	server, root := commandServer(t)

	if _, err := server.DeclareCommand(protocol.DeclareCommandRequest{
		Name: "chained", Run: "echo one > a.txt && echo two >> a.txt",
	}); err != nil {
		t.Fatalf("DeclareCommand: %v", err)
	}
	if _, err := server.RunCommand(protocol.RunCommandRequest{Name: "chained", Wait: true}); err != nil {
		t.Fatalf("RunCommand: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "a.txt"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), "one") || !strings.Contains(string(data), "two") {
		t.Fatalf("expected both halves of the chain to run, got %q", data)
	}
}

func TestRunCommandReportsFailure(t *testing.T) {
	server, _ := commandServer(t)

	if _, err := server.DeclareCommand(protocol.DeclareCommandRequest{
		Name: "nope", Run: "echo 'FAIL: broken' && exit 1",
	}); err != nil {
		t.Fatalf("DeclareCommand: %v", err)
	}

	response, err := server.RunCommand(protocol.RunCommandRequest{Name: "nope", Wait: true})
	if err != nil {
		t.Fatalf("a failing command is a verdict, not a tool error: %v", err)
	}
	if response.Passed {
		t.Fatalf("expected a failing command to report failure, got %+v", response)
	}
	if response.Summary == "" {
		t.Fatalf("expected the failure output carried back")
	}
}

func TestUnknownCommandNamesTheOnesThatExist(t *testing.T) {
	// The teachable error is the reason a name beats a shell string.
	server, _ := commandServer(t)
	if _, err := server.DeclareCommand(protocol.DeclareCommandRequest{Name: "lint", Run: "true"}); err != nil {
		t.Fatalf("DeclareCommand: %v", err)
	}

	_, err := server.RunCommand(protocol.RunCommandRequest{Name: "lnit", Wait: true})
	if err == nil {
		t.Fatalf("expected an unknown command to be rejected")
	}
	if !strings.Contains(err.Error(), "lint") {
		t.Fatalf("expected the error to list what is declared, got %v", err)
	}
}

func TestDeclareReportsReplacementThroughTheTransport(t *testing.T) {
	server, _ := commandServer(t)
	if _, err := server.DeclareCommand(protocol.DeclareCommandRequest{Name: "build", Run: "go build ./..."}); err != nil {
		t.Fatalf("DeclareCommand: %v", err)
	}

	response, err := server.DeclareCommand(protocol.DeclareCommandRequest{Name: "build", Run: "make build"})
	if err != nil {
		t.Fatalf("DeclareCommand: %v", err)
	}
	if !response.Replaced {
		t.Fatalf("expected the overwrite surfaced, got %+v", response)
	}
	if response.Run != "make build" {
		t.Fatalf("expected the new run string reported, got %q", response.Run)
	}
}

func TestDeclareBumpsTheRevision(t *testing.T) {
	// The registry is a workspace change like any other. Leaving the revision
	// alone would let an edit preconditioned on a stale revision sail through
	// after the command surface had changed underneath it.
	server, _ := commandServer(t)
	before := server.workspace.Revision()

	if _, err := server.DeclareCommand(protocol.DeclareCommandRequest{Name: "lint", Run: "true"}); err != nil {
		t.Fatalf("DeclareCommand: %v", err)
	}
	if after := server.workspace.Revision(); after == before {
		t.Fatalf("expected the revision to move, stayed at %s", before)
	}
}

func TestRemoveCommand(t *testing.T) {
	server, _ := commandServer(t)
	if _, err := server.DeclareCommand(protocol.DeclareCommandRequest{Name: "temp", Run: "true"}); err != nil {
		t.Fatalf("DeclareCommand: %v", err)
	}

	response, err := server.DeclareCommand(protocol.DeclareCommandRequest{Name: "temp", Remove: true})
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !response.Removed {
		t.Fatalf("expected the removal reported, got %+v", response)
	}

	if _, err := server.RunCommand(protocol.RunCommandRequest{Name: "temp", Wait: true}); err == nil {
		t.Fatalf("expected the removed command to be gone")
	}
}

func TestDeclareRejectsAnInvalidNameBeforeWritingAnything(t *testing.T) {
	server, root := commandServer(t)

	if _, err := server.DeclareCommand(protocol.DeclareCommandRequest{Name: "rm -rf /", Run: "true"}); err == nil {
		t.Fatalf("expected an invalid name to be rejected")
	}
	if _, err := os.Stat(filepath.Join(root, commands.RelPath)); err == nil {
		t.Fatalf("expected no registry file to be created by a rejected declaration")
	}
}
func TestChangesSkipsSymbolDeltaOnALargeBranch(t *testing.T) {
	// Parsing both sides of every changed file measured at 704ms on 88 files.
	// Past the cutoff the renderer's 40-symbol cap would reduce the result to
	// an arbitrary sample anyway, and an arbitrary sample presented as a
	// summary is worse than none — it reads like the whole answer.
	server, root := commandServer(t)

	// Seeded through arno's own edit ledger rather than git, so the test does
	// not need a repository to exercise a cutoff that is about file count.
	names := make([]string, 0, maxSymbolDeltaFiles+5)
	for i := 0; i < maxSymbolDeltaFiles+5; i++ {
		name := fmt.Sprintf("file%03d.go", i)
		body := fmt.Sprintf("package probe\n\nfunc F%03d() {}\n", i)
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		names = append(names, name)
	}
	server.workspace.BumpRevision(names...)

	response := server.Changes()
	if len(response.Files) <= maxSymbolDeltaFiles {
		t.Fatalf("fixture did not exceed the cutoff: %d files", len(response.Files))
	}
	if response.SymbolsOmitted != len(response.Files) {
		t.Fatalf("expected the omission recorded with the file count, got %d", response.SymbolsOmitted)
	}
	if len(response.Symbols) != 0 {
		t.Fatalf("expected no symbol delta computed, got %d", len(response.Symbols))
	}
}

func TestChangesStillComputesSymbolDeltaOnASmallBranch(t *testing.T) {
	// The cutoff must not cost the feature in the case it was built for.
	server, root := commandServer(t)

	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package probe\n\nfunc Added() {}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	server.workspace.BumpRevision("a.go")

	response := server.Changes()
	if len(response.Files) == 0 {
		t.Fatalf("fixture produced no changed files")
	}
	if response.SymbolsOmitted != 0 {
		t.Fatalf("expected no omission below the cutoff, got %d", response.SymbolsOmitted)
	}
}
func TestRunCommandFailsOnExitStatusAloneEndToEnd(t *testing.T) {
	// The regression test for the release blocker, driven through the real
	// runner rather than a constructed JobOutput — the bug was that the exit
	// status never reached the verdict, so a test that hands the verdict an
	// exit status could not have caught it.
	server, _ := commandServer(t)

	if _, err := server.DeclareCommand(protocol.DeclareCommandRequest{
		Name: "cheerful-failure",
		Run:  "echo 'everything looks fine'; exit 3",
	}); err != nil {
		t.Fatalf("DeclareCommand: %v", err)
	}

	response, err := server.RunCommand(protocol.RunCommandRequest{Name: "cheerful-failure", Wait: true})
	if err != nil {
		t.Fatalf("a failing command is a verdict, not a tool error: %v", err)
	}
	if response.Passed {
		t.Fatalf("expected exit 3 to fail despite reassuring output, got %+v", response)
	}
}

func TestRunCommandStillPassesOnCleanExit(t *testing.T) {
	// The other half: making exit status authoritative must not make every
	// command look broken.
	server, _ := commandServer(t)

	if _, err := server.DeclareCommand(protocol.DeclareCommandRequest{
		Name: "quiet-success",
		Run:  "echo 'done'; exit 0",
	}); err != nil {
		t.Fatalf("DeclareCommand: %v", err)
	}

	response, err := server.RunCommand(protocol.RunCommandRequest{Name: "quiet-success", Wait: true})
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	if !response.Passed {
		t.Fatalf("expected a clean exit to pass, got %+v", response)
	}
}

func TestRunCommandFailsWhenOutputSaysSoDespiteCleanExit(t *testing.T) {
	// Some tools report a failure and exit 0 anyway, so the text scan is kept
	// as a secondary signal — it can fail a job, never pass one.
	server, _ := commandServer(t)

	if _, err := server.DeclareCommand(protocol.DeclareCommandRequest{
		Name: "silent-liar",
		Run:  "echo 'FAIL: 2 checks did not pass'; exit 0",
	}); err != nil {
		t.Fatalf("DeclareCommand: %v", err)
	}

	response, err := server.RunCommand(protocol.RunCommandRequest{Name: "silent-liar", Wait: true})
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	if response.Passed {
		t.Fatalf("expected failure wording to still count, got %+v", response)
	}
}
func TestAmbiguousReadSymbolCarriesSignaturesEndToEnd(t *testing.T) {
	// Reproduces the case that cost ARNO a benchmark head-to-head: two
	// methods with the same name in one file, distinguishable only by receiver.
	// The signature has to be read off disk, so this is driven through the real
	// index rather than a constructed response.
	server, root := commandServer(t)
	server.index = code.NewIndex(root, nil)

	source := "package probe\n\n" +
		"type S3CompatibleStore struct{}\n\n" +
		"func (s *S3CompatibleStore) Put(name string) error { return nil }\n\n" +
		"type Store struct{}\n\n" +
		"func (s *Store) Put(name string) error { return nil }\n"
	if err := os.WriteFile(filepath.Join(root, "store.go"), []byte(source), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	response, err := server.ReadSymbol(protocol.ReadSymbolRequest{Path: "store.go", SymbolName: "Put"})
	if err != nil {
		t.Fatalf("ReadSymbol: %v", err)
	}
	if response.Resolve.Status != protocol.ResolutionAmbiguous {
		t.Fatalf("expected ambiguity, got %+v", response.Resolve)
	}
	if len(response.Resolve.Candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %+v", response.Resolve.Candidates)
	}

	var receivers []string
	for _, candidate := range response.Resolve.Candidates {
		if candidate.Signature == "" {
			t.Fatalf("candidate %s has no signature", candidate.ID)
		}
		receivers = append(receivers, candidate.Signature)
	}
	joined := strings.Join(receivers, "\n")
	if !strings.Contains(joined, "S3CompatibleStore") || !strings.Contains(joined, "(s *Store)") {
		t.Fatalf("expected both receivers readable from the response, got:\n%s", joined)
	}
}

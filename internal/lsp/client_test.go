package lsp

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These tests run against a fake language server built from this package's own
// test binary. Real servers are not available on every machine and are slow
// and non-deterministic where they are — but the behaviours that actually
// matter here are protocol behaviours (a server that hangs, one that dies
// mid-request, one that asks jade a question), and those are only reliably
// producible from a server written to produce them.
func TestMain(m *testing.M) {
	if script := os.Getenv("JADE_FAKE_LSP"); script != "" {
		runFakeServer(script)
		return
	}
	os.Exit(m.Run())
}

// fakeSpec returns a ServerSpec that re-executes this test binary in fake
// server mode, with behaviour selected by script.
func fakeSpec(t *testing.T, script string) ServerSpec {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("JADE_FAKE_LSP", script)
	return ServerSpec{
		Language:   "fake",
		Command:    self,
		LanguageID: "fake",
	}
}

func startFake(t *testing.T, script string) *Client {
	t.Helper()
	spec := fakeSpec(t, script)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := Start(ctx, spec, t.TempDir())
	if err != nil {
		t.Fatalf("start fake server (%s): %v", script, err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestHandshakeRecordsCapabilities(t *testing.T) {
	client := startFake(t, "normal")

	if !client.Supports("referencesProvider") {
		t.Error("referencesProvider should be supported")
	}
	if !client.Supports("renameProvider") {
		t.Error("renameProvider is declared as an options object and must count as supported")
	}
	if client.Supports("codeLensProvider") {
		t.Error("a capability the server declared false must not count as supported")
	}
	if client.Supports("somethingNeverMentioned") {
		t.Error("an undeclared capability must not count as supported")
	}
}

// The failure that matters most: a server that stops answering must cost one
// bounded wait, not a hung tool call.
func TestRequestToAHangingServerTimesOut(t *testing.T) {
	client := startFake(t, "hang")

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	started := time.Now()
	err := client.Call(ctx, "textDocument/references", ReferenceParams{}, new([]Location))
	elapsed := time.Since(started)

	if err == nil {
		t.Fatal("a hanging server must produce an error, not a result")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("the call outlived its context by too much: %v", elapsed)
	}
}

// A server that dies must wake its waiting callers immediately rather than
// leaving them to time out against a dead pipe.
func TestCallFailsFastWhenTheServerDies(t *testing.T) {
	client := startFake(t, "die")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	started := time.Now()
	err := client.Call(ctx, "textDocument/references", ReferenceParams{}, new([]Location))
	elapsed := time.Since(started)

	if err == nil {
		t.Fatal("expected an error from a server that exited")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("expected to be woken by the process exit, waited %v", elapsed)
	}
}

// A server-to-client request left unanswered blocks the server forever, which
// presents as a hang with no error anywhere. This asserts jade answers.
func TestServerToClientRequestIsAnswered(t *testing.T) {
	client := startFake(t, "asks")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var reply struct {
		Answered bool `json:"answered"`
	}
	if err := client.Call(ctx, "custom/didYouAnswerMe", map[string]any{}, &reply); err != nil {
		t.Fatalf("call failed: %v", err)
	}
	if !reply.Answered {
		t.Fatal("jade must answer a server-to-client request, or the server waits forever")
	}
}

func TestDiagnosticsArePublishedNotRequested(t *testing.T) {
	client := startFake(t, "diagnostics")
	path := filepath.Join(t.TempDir(), "a.go")

	// The fake publishes on didOpen, so trigger one.
	_ = client.Notify("textDocument/didOpen", DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{URI: pathToURI(path), LanguageID: "go", Version: 1, Text: "x"},
	})

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if found := client.Diagnostics(path); len(found) > 0 {
			if found[0].Severity != SeverityError {
				t.Fatalf("expected an error severity, got %d", found[0].Severity)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no diagnostics arrived")
}

func TestCloseShutsTheProcessDown(t *testing.T) {
	client := startFake(t, "normal")
	pid := client.cmd.Process.Pid

	if err := client.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// Close is idempotent: the manager may close a client the caller already
	// closed, and a double close must not panic or hang.
	if err := client.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}

	if processAlive(pid) {
		t.Fatal("the server process outlived Close")
	}
}

func processAlive(pid int) bool {
	// Signal 0 checks for existence without delivering anything.
	return exec.Command("kill", "-0", itoa(pid)).Run() == nil
}

func itoa(n int) string {
	return strings.TrimSpace(string(json.RawMessage(jsonNumber(n))))
}

func jsonNumber(n int) []byte {
	raw, _ := json.Marshal(n)
	return raw
}

// An indexing server answers wrongly rather than slowly, so the only safe
// moment to ask is after its progress token ends. Waiting out the grace
// period alone would return mid-index here.
func TestWaitSettledWaitsForProgressToEnd(t *testing.T) {
	client := startFake(t, "indexing")

	started := time.Now()
	client.WaitSettled(context.Background())
	elapsed := time.Since(started)

	if elapsed < 2*fakeIndexingStep {
		t.Fatalf("returned after %v, before the server ended its progress token", elapsed)
	}
	if elapsed > SettleTimeout/2 {
		t.Fatalf("waited %v; should have returned soon after progress ended", elapsed)
	}
	client.progressMu.Lock()
	defer client.progressMu.Unlock()
	if len(client.activeProgress) != 0 {
		t.Fatalf("progress still active after settling: %v", client.activeProgress)
	}
}

// A server that reports no progress must cost the grace period once, not the
// settle timeout.
func TestWaitSettledDoesNotStallOnAQuietServer(t *testing.T) {
	client := startFake(t, "normal")

	started := time.Now()
	client.WaitSettled(context.Background())
	if elapsed := time.Since(started); elapsed > settleGrace+time.Second {
		t.Fatalf("a server with no progress to report cost %v", elapsed)
	}

	started = time.Now()
	client.WaitSettled(context.Background())
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("an already settled server cost %v on the second call", elapsed)
	}
}

// Diagnostics read straight after a change are the previous content's. The
// generation tells a fresh publish from a stale one.
func TestWaitForDiagnosticsWaitsForAFreshPublish(t *testing.T) {
	client := startFake(t, "diagnostics")
	path := filepath.Join(t.TempDir(), "a.go")

	before := client.DiagnosticsGeneration(path)
	_ = client.Notify("textDocument/didOpen", DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{URI: pathToURI(path), LanguageID: "go", Version: 1, Text: "x"},
	})

	published, fresh := client.WaitForDiagnostics(context.Background(), path, before, 3*time.Second)
	if !fresh || len(published) != 1 {
		t.Fatalf("expected the fresh publish, got %v %v", published, fresh)
	}

	// Nothing new is coming: the wait must report that, promptly, rather than
	// handing back the old list as if it were current.
	started := time.Now()
	if _, fresh := client.WaitForDiagnostics(context.Background(), path, client.DiagnosticsGeneration(path), 200*time.Millisecond); fresh {
		t.Fatal("reported a fresh publish that never happened")
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("waited %v for a 200ms timeout", elapsed)
	}
	if client.Command() == "" {
		t.Fatal("client should name the binary it launched")
	}
}

// ruby-lsp's model: diagnostics are requested, never published.
func TestPullDiagnosticsAsksTheServer(t *testing.T) {
	client := startFake(t, "pull")
	path := filepath.Join(t.TempDir(), "a.rb")

	items, ok := PullDiagnostics(context.Background(), client, path)
	if !ok || len(items) != 1 || items[0].Message != "pulled, not published" {
		t.Fatalf("expected the pulled diagnostic, got %v %v", items, ok)
	}
	if !client.WantsSave() {
		t.Fatal("a save options object must count as wanting didSave")
	}
}

func TestPullIsNotAttemptedWithoutTheCapability(t *testing.T) {
	client := startFake(t, "normal")
	if _, ok := PullDiagnostics(context.Background(), client, "a.go"); ok {
		t.Fatal("pulled from a server that does not advertise diagnosticProvider")
	}
	if client.WantsSave() {
		t.Fatal("a server with no textDocumentSync save option does not want didSave")
	}
}

// metals' model: an empty publish on change, the real result a moment later.
// Taking the first publish reports a broken file as clean.
func TestWaitForDiagnosticsTakesTheSettledPublishNotTheFirst(t *testing.T) {
	client := startFake(t, "settling")
	path := filepath.Join(t.TempDir(), "A.scala")

	before := client.DiagnosticsGeneration(path)
	_ = client.Notify("textDocument/didOpen", DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{URI: pathToURI(path), LanguageID: "scala", Version: 1, Text: "x"},
	})
	published, fresh := client.WaitForDiagnostics(context.Background(), path, before, 3*time.Second)
	if !fresh || len(published) != 1 || published[0].Message != "the real answer" {
		t.Fatalf("expected the settled publish, got %v %v", published, fresh)
	}
	if client.WantsSave() {
		t.Fatal("a bare sync kind carries no save option")
	}
}

// A server still importing a build has not looked at the edit yet. Whatever it
// published is not an answer, and the wait must say so rather than return it.
func TestWaitForDiagnosticsRefusesAnAnswerFromABusyServer(t *testing.T) {
	client := startFake(t, "diagnostics")
	path := filepath.Join(t.TempDir(), "A.scala")

	client.progressMu.Lock()
	client.activeProgress[`"import"`] = "Importing build"
	client.progressMu.Unlock()

	before := client.DiagnosticsGeneration(path)
	_ = client.Notify("textDocument/didOpen", DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{URI: pathToURI(path), LanguageID: "scala", Version: 1, Text: "x"},
	})
	if _, fresh := client.WaitForDiagnostics(context.Background(), path, before, 800*time.Millisecond); fresh {
		t.Fatal("returned diagnostics while the server reported it was still importing")
	}
	if busy := client.Busy(); busy != "Importing build" {
		t.Fatalf("Busy() = %q", busy)
	}
}

// A compile-on-save server's empty publish before it compiles must not be
// taken as a clean result; only diagnostics that follow completed work count.
func TestWaitForCompiledDiagnosticsNeedsWorkAfterTheChange(t *testing.T) {
	client := startFake(t, "normal")
	path := filepath.Join(t.TempDir(), "A.scala")
	uri := pathToURI(path)

	client.diagnosticsMu.Lock()
	client.diagnostics[uri] = []Diagnostic{}
	client.diagnosticsGeneration[uri]++
	client.diagnosticsMu.Unlock()

	endedBefore := client.ProgressEnded()
	if _, ok := client.WaitForCompiledDiagnostics(context.Background(), path, endedBefore, 600*time.Millisecond); ok {
		t.Fatal("accepted an empty publish with no compile after the change")
	}

	// The compile: a token begins and ends, then the real result is published.
	go func() {
		client.progressMu.Lock()
		client.activeProgress[`"compile"`] = "Compiling scala"
		client.lastProgress = time.Now()
		client.progressMu.Unlock()
		time.Sleep(100 * time.Millisecond)

		client.diagnosticsMu.Lock()
		client.diagnostics[uri] = []Diagnostic{{Severity: SeverityError, Message: "identifier expected"}}
		client.diagnosticsGeneration[uri]++
		client.diagnosticsMu.Unlock()

		client.progressMu.Lock()
		delete(client.activeProgress, `"compile"`)
		client.progressEnded++
		client.lastProgress = time.Now()
		client.progressMu.Unlock()
	}()

	published, ok := client.WaitForCompiledDiagnostics(context.Background(), path, endedBefore, 3*time.Second)
	if !ok || len(published) != 1 || published[0].Message != "identifier expected" {
		t.Fatalf("expected the post-compile diagnostics, got %v %v", published, ok)
	}
}

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

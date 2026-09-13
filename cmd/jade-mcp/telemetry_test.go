package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/julianbei/jade/internal/code"
	"github.com/julianbei/jade/internal/diagnostics"
	"github.com/julianbei/jade/internal/edit"
	"github.com/julianbei/jade/internal/events"
	"github.com/julianbei/jade/internal/jobs"
	"github.com/julianbei/jade/internal/languages"
	"github.com/julianbei/jade/internal/telemetry"
	"github.com/julianbei/jade/internal/transport/internalapi"
	"github.com/julianbei/jade/internal/workspace"
)

// newTestMCPServer wires the same graph main() does, against a temp workspace.
// The point of testing at this level rather than calling the Recorder directly
// is that the wiring is the part that can silently be wrong: a recorder that
// works perfectly but is never called measures nothing.
func newTestMCPServer(t *testing.T) (*mcpServer, string) {
	t.Helper()
	root := t.TempDir()

	bus := events.NewBus()
	wm := workspace.NewManager(root, bus)
	ci := code.NewIndex(root, bus)
	ds := diagnostics.NewService(root)
	jr := jobs.NewRunner(bus)
	es := edit.NewService(wm, ci, ds, jr)
	lr := languages.NewRegistry()

	api := internalapi.NewServer(wm, ci, es, ds, jr, lr, bus)
	return &mcpServer{api: api, telemetry: telemetry.New(root)}, root
}

func toolCall(t *testing.T, name string, args map[string]interface{}) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]interface{}{"name": name, "arguments": args})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

func TestToolCallsAreRecordedThroughTheRealDispatchPath(t *testing.T) {
	server, root := newTestMCPServer(t)

	if _, err := server.handleToolCall(toolCall(t, "jade.changes", nil)); err != nil {
		t.Fatalf("changes: %v", err)
	}

	summary, err := telemetry.New(root).Summarize()
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if summary.TotalCalls != 1 {
		t.Fatalf("expected the call recorded, got %d", summary.TotalCalls)
	}
	if summary.Tools[0].Tool != "jade.changes" {
		t.Fatalf("expected the tool name recorded, got %+v", summary.Tools)
	}
	if summary.Tools[0].Bytes == 0 {
		t.Fatalf("expected the rendered response size recorded, got %+v", summary.Tools[0])
	}
}

func TestFailedToolCallsAreRecordedWithTheirClass(t *testing.T) {
	// The measurement the package exists for: a failure here is a moment the
	// caller may have gone to the shell instead.
	server, root := newTestMCPServer(t)

	if _, err := server.handleToolCall(toolCall(t, "jade.job_status", map[string]interface{}{
		"jobId": "job-does-not-exist",
	})); err == nil {
		t.Fatalf("expected the call to fail")
	}

	summary, err := telemetry.New(root).Summarize()
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if summary.TotalErrors != 1 {
		t.Fatalf("expected one error recorded, got %+v", summary)
	}
	if len(summary.Fallbacks) == 0 {
		t.Fatalf("expected a failure class recorded, got %+v", summary)
	}
}

func TestUnknownToolIsRecordedToo(t *testing.T) {
	// A call for a tool that does not exist is itself a signal — it means the
	// agent expected a capability jade does not have, which is the most direct
	// evidence of a gap this log can carry. It is recorded into an existing
	// log only; TestUnknownToolWritesNothingToTheWorkspace covers the case
	// where no log exists yet.
	server, root := newTestMCPServer(t)
	if err := os.MkdirAll(filepath.Join(root, ".jade"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := server.handleToolCall(toolCall(t, "jade.does_not_exist", nil)); err == nil {
		t.Fatalf("expected an unknown tool to fail")
	}

	summary, _ := telemetry.New(root).Summarize()
	if summary.TotalCalls != 1 || summary.Tools[0].Tool != "jade.does_not_exist" {
		t.Fatalf("expected the unknown tool recorded by name, got %+v", summary.Tools)
	}
}

func TestTelemetryToolReportsWhatWasRecorded(t *testing.T) {
	// End to end: the log is readable back through the same tool surface that
	// produced it. Telemetry nobody can read is telemetry nobody acts on.
	server, _ := newTestMCPServer(t)

	for i := 0; i < 3; i++ {
		if _, err := server.handleToolCall(toolCall(t, "jade.changes", nil)); err != nil {
			t.Fatalf("changes: %v", err)
		}
	}

	result, err := server.handleToolCall(toolCall(t, "jade.telemetry", nil))
	if err != nil {
		t.Fatalf("telemetry: %v", err)
	}
	if len(result.Content) == 0 {
		t.Fatalf("expected rendered content")
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "jade.changes") {
		t.Fatalf("expected the recorded tool in the report, got:\n%s", text)
	}
	if !strings.Contains(text, "3 calls") {
		t.Fatalf("expected the call count, got:\n%s", text)
	}
}

func TestTelemetryResetClearsTheLogThroughTheTool(t *testing.T) {
	server, root := newTestMCPServer(t)

	if _, err := server.handleToolCall(toolCall(t, "jade.changes", nil)); err != nil {
		t.Fatalf("changes: %v", err)
	}
	if _, err := server.handleToolCall(toolCall(t, "jade.telemetry", map[string]interface{}{
		"reset": true,
	})); err != nil {
		t.Fatalf("reset: %v", err)
	}

	// The reset call itself is recorded after the clear, so exactly one
	// record should remain rather than zero.
	summary, _ := telemetry.New(root).Summarize()
	if summary.TotalCalls > 1 {
		t.Fatalf("expected the log cleared, got %d calls", summary.TotalCalls)
	}
}

func TestTelemetryWritesOnlyUnderDotJade(t *testing.T) {
	// The log goes in one predictable place. A telemetry file appearing
	// somewhere unexpected in a user's repository would be a surprise worth
	// failing a test over.
	server, root := newTestMCPServer(t)
	if _, err := server.handleToolCall(toolCall(t, "jade.changes", nil)); err != nil {
		t.Fatalf("changes: %v", err)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() != telemetry.Dir {
			t.Fatalf("unexpected entry %q created in the workspace root", entry.Name())
		}
	}
	if _, err := os.Stat(filepath.Join(root, telemetry.RelPath)); err != nil {
		t.Fatalf("expected the log at %s: %v", telemetry.RelPath, err)
	}
}
func TestInBandFailureIsRecordedAsAFallback(t *testing.T) {
	// The miss 13.2's own tests exposed: read_symbol reports "not found" as a
	// field on a successful response rather than as an error, so the recorder
	// counted it OK and understated the number it exists to measure. This is
	// the regression test for that, driven through the real dispatch path
	// because the bug lived in the wiring, not the classifier.
	server, root := newTestMCPServer(t)
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package probe\n\nfunc Present() {}\n"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	if _, err := server.handleToolCall(toolCall(t, "jade.read_symbol", map[string]interface{}{
		"path":       "a.go",
		"symbolName": "DefinitelyNotDeclaredHere",
	})); err != nil {
		// The point is precisely that this does NOT error.
		t.Fatalf("expected an in-band not_found, got a returned error: %v", err)
	}

	summary, err := telemetry.New(root).Summarize()
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if summary.TotalErrors != 1 {
		t.Fatalf("expected the in-band failure counted as an error, got %+v", summary)
	}
	if len(summary.Fallbacks) == 0 {
		t.Fatalf("expected a fallback class recorded, got %+v", summary)
	}
}

func TestAFailingCheckIsNotRecordedAsAFallback(t *testing.T) {
	// A red build is jade working, not jade failing. If this ever starts
	// counting, the fallback metric stops meaning anything.
	server, root := newTestMCPServer(t)

	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module probe\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "broken.go"), []byte("package probe\n\nfunc Broken() { this is not go }\n"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	if _, err := server.handleToolCall(toolCall(t, "jade.check", map[string]interface{}{
		"kind": "build",
	})); err != nil {
		t.Fatalf("check itself should succeed even when the build fails: %v", err)
	}

	summary, err := telemetry.New(root).Summarize()
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if summary.TotalCalls != 1 {
		t.Fatalf("expected the call recorded, got %d", summary.TotalCalls)
	}
	if summary.TotalErrors != 0 {
		t.Fatalf("expected a failing build NOT to count as a jade failure, got %+v", summary.Fallbacks)
	}
}
func TestVersionIsStampedOrHonestlyDev(t *testing.T) {
	// "dev" is the correct answer for a plain `go build` or `go run`, and it is
	// useful information rather than a placeholder: it says the server was not
	// built through the release path. What must never happen is an empty
	// string, which would make `--version` look broken.
	if version == "" {
		t.Fatalf("expected a version string, got empty")
	}
}

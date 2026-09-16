package telemetry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/julianbei/arno/internal/protocol"
)

func TestRecordAndSummarize(t *testing.T) {
	root := t.TempDir()
	recorder := New(root)

	recorder.Record("arno.find", 12*time.Millisecond, 300, nil)
	recorder.Record("arno.find", 8*time.Millisecond, 100, nil)
	recorder.Record("arno.apply", 50*time.Millisecond, 900, nil)

	summary, err := recorder.Summarize()
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if summary.TotalCalls != 3 {
		t.Fatalf("expected 3 calls, got %d", summary.TotalCalls)
	}
	if summary.TotalBytes != 1300 {
		t.Fatalf("expected 1300 bytes, got %d", summary.TotalBytes)
	}
	// Ordered by call count descending, so the ranking leads.
	if summary.Tools[0].Tool != "arno.find" || summary.Tools[0].Calls != 2 {
		t.Fatalf("expected find first with 2 calls, got %+v", summary.Tools)
	}
	if summary.Tools[0].MaxMS != 12 {
		t.Fatalf("expected the slowest call recorded, got %d", summary.Tools[0].MaxMS)
	}
}

func TestSummarizeCountsFailureClassesSeparately(t *testing.T) {
	// The headline number: each of these is a moment the arno path failed and
	// the shell was one keystroke away.
	root := t.TempDir()
	recorder := New(root)

	recorder.Record("arno.replace_text", time.Millisecond, 10, errors.New("anchor text is ambiguous: 3 matches"))
	recorder.Record("arno.replace_text", time.Millisecond, 10, errors.New("anchor text not found in a.go"))
	recorder.Record("arno.replace_text", time.Millisecond, 10, nil)

	summary, err := recorder.Summarize()
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if summary.TotalErrors != 2 {
		t.Fatalf("expected 2 errors, got %d", summary.TotalErrors)
	}
	if summary.TotalCalls != 3 {
		t.Fatalf("expected successes counted too, got %d", summary.TotalCalls)
	}

	classes := map[Outcome]int{}
	for _, failure := range summary.Fallbacks {
		classes[failure.Outcome] = failure.Count
	}
	if classes[Ambiguous] != 1 || classes[NotFound] != 1 {
		t.Fatalf("expected one of each class, got %+v", summary.Fallbacks)
	}
}

func TestRecordsNoArgumentsResponsesOrErrorText(t *testing.T) {
	// The property that makes the log shareable, and therefore useful. A file
	// containing source code could not be attached to a bug report or
	// committed, which would defeat the reason for collecting it.
	root := t.TempDir()
	recorder := New(root)

	secret := "func TopSecret() { apiKey := \"sk-live-12345\" }"
	recorder.Record("arno.replace_text", time.Millisecond, len(secret), fmt.Errorf("anchor text not found: %s", secret))

	data, err := os.ReadFile(filepath.Join(root, RelPath))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, leak := range []string{"TopSecret", "sk-live-12345", "apiKey"} {
		if strings.Contains(string(data), leak) {
			t.Fatalf("telemetry leaked %q into the log:\n%s", leak, data)
		}
	}
	// The classification still has to survive, or the redaction cost the point.
	if !strings.Contains(string(data), string(NotFound)) {
		t.Fatalf("expected the failure class recorded, got:\n%s", data)
	}
}

func TestDisabledRecorderWritesNothing(t *testing.T) {
	// An off switch is not optional for something that writes into the user's
	// repository on every call.
	root := t.TempDir()
	t.Setenv("ARNO_TELEMETRY", "0")

	recorder := New(root)
	recorder.Record("arno.find", time.Millisecond, 10, nil)

	if _, err := os.Stat(filepath.Join(root, RelPath)); err == nil {
		t.Fatalf("expected no log file when telemetry is disabled")
	}
}

func TestRecordNeverFailsTheCall(t *testing.T) {
	// Losing a measurement is cheap; failing a working edit because the
	// measurement could not be written would make arno less reliable than the
	// bash it competes with. An unwritable root must be swallowed.
	root := filepath.Join(t.TempDir(), "does", "not", "exist", "\x00bad")
	recorder := New(root)

	// Must not panic, must not block, must not return anything to fail on.
	recorder.Record("arno.find", time.Millisecond, 10, nil)
}

func TestSummarizeReportsAnUnreadableLogRatherThanClaimingItIsEmpty(t *testing.T) {
	// The asymmetry with Record above is deliberate. Recording is a side effect
	// nobody asked for, so it fails silently. Summarizing was explicitly
	// requested, and answering "no tool calls recorded yet" when the log exists
	// but could not be read would be a confident lie about the one thing the
	// caller wanted to know.
	root := filepath.Join(t.TempDir(), "\x00bad")
	if _, err := New(root).Summarize(); err == nil {
		t.Fatalf("expected an unreadable log to be reported, not silently empty")
	}
}

func TestSummarizeOnMissingLogIsEmptyNotAnError(t *testing.T) {
	summary, err := New(t.TempDir()).Summarize()
	if err != nil {
		t.Fatalf("expected a missing log to summarize empty, got %v", err)
	}
	if summary.TotalCalls != 0 {
		t.Fatalf("expected no calls, got %d", summary.TotalCalls)
	}
}

func TestReadSkipsAMalformedLine(t *testing.T) {
	// A partially written record from a killed process must not make the whole
	// history unreadable.
	root := t.TempDir()
	recorder := New(root)
	recorder.Record("arno.find", time.Millisecond, 10, nil)

	path := filepath.Join(root, RelPath)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := file.WriteString("{\"tool\": \"truncated\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	file.Close()

	recorder.Record("arno.apply", time.Millisecond, 10, nil)

	summary, err := recorder.Summarize()
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if summary.TotalCalls != 2 {
		t.Fatalf("expected the two good records to survive, got %d", summary.TotalCalls)
	}
}

func TestResetClearsTheLog(t *testing.T) {
	root := t.TempDir()
	recorder := New(root)
	recorder.Record("arno.find", time.Millisecond, 10, nil)

	if err := recorder.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	summary, _ := recorder.Summarize()
	if summary.TotalCalls != 0 {
		t.Fatalf("expected an empty log after reset, got %d", summary.TotalCalls)
	}
	// Resetting an already-absent log is not an error.
	if err := recorder.Reset(); err != nil {
		t.Fatalf("expected a second reset to succeed, got %v", err)
	}
}

func TestConcurrentRecordsAllLand(t *testing.T) {
	root := t.TempDir()
	recorder := New(root)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			recorder.Record("arno.find", time.Millisecond, 10, nil)
		}()
	}
	wg.Wait()

	summary, err := recorder.Summarize()
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if summary.TotalCalls != 50 {
		t.Fatalf("expected 50 records, got %d — interleaved writes", summary.TotalCalls)
	}
}

func TestClassify(t *testing.T) {
	cases := map[string]Outcome{
		"anchor text is ambiguous: 3 matches":          Ambiguous,
		"stale revision":                               StaleRevision,
		"expected revision r4, got r7":                 StaleRevision,
		"job timed out after 60s":                      Timeout,
		"context deadline exceeded":                    Timeout,
		"anchor text not found in a.go":                NotFound,
		"no such file or directory":                    NotFound,
		"no declarations matching Foo":                 NotFound,
		"gopls is not installed":                       Unavailable,
		"exec: \"rustfmt\": executable file not found": Unavailable,
		"search text is required":                      InvalidInput,
		"unknown op \"rm -rf\"":                        InvalidInput,
		"invalid regex \"func (\"":                     InvalidInput,
		"something nobody has classified yet":          Other,
	}
	for message, want := range cases {
		if got := Classify(errors.New(message)); got != want {
			t.Fatalf("Classify(%q) = %q, want %q", message, got, want)
		}
	}
	if got := Classify(nil); got != OK {
		t.Fatalf("Classify(nil) = %q, want ok", got)
	}
}

func TestSummaryLineReadsCleanlyWhenEmpty(t *testing.T) {
	if line := SummaryLine(Summary{}); !strings.Contains(line, "no tool calls") {
		t.Fatalf("expected a clear empty summary, got %q", line)
	}
}
func TestClassifyResponseReadsInBandFailures(t *testing.T) {
	// read_symbol reports "not found" as a field on a successful response,
	// because returning the near-miss candidates alongside is more useful than
	// an error that throws them away. The call still failed for the caller, and
	// counting it as OK understated the exact number this package measures.
	cases := map[protocol.SymbolResolutionStatus]Outcome{
		protocol.ResolutionNotFound:  NotFound,
		protocol.ResolutionAmbiguous: Ambiguous,
		protocol.ResolutionExact:     OK,
	}
	for status, want := range cases {
		got, found := ClassifyResponse(protocol.InspectResponse{
			Resolve: protocol.SymbolResolution{Status: status},
		})
		if !found {
			t.Fatalf("status %q: expected an in-band outcome to be found", status)
		}
		if got != want {
			t.Fatalf("status %q = %q, want %q", status, got, want)
		}
	}
}

func TestClassifyResponseIgnoresResponsesWithNothingToSay(t *testing.T) {
	if _, found := ClassifyResponse(protocol.ChangesResponse{}); found {
		t.Fatalf("expected no in-band outcome on a response that carries none")
	}
	if _, found := ClassifyResponse(nil); found {
		t.Fatalf("expected nil to be handled")
	}
}

func TestAFailingBuildIsAVerdictNotAFallback(t *testing.T) {
	// The distinction the whole metric depends on. A red build is arno doing
	// its job, not arno failing; counting it would swamp the fallback signal
	// with ordinary broken code and make the number meaningless.
	for _, response := range []interface{}{
		protocol.CheckResponse{Status: "completed", Passed: false},
		protocol.RunTestsResponse{Status: "completed", Passed: false},
		protocol.RunCommandResponse{Status: "completed", Passed: false},
	} {
		if outcome, found := ClassifyResponse(response); found || outcome != OK {
			t.Fatalf("%T: expected a failing verdict not to count as a fallback, got %q", response, outcome)
		}
	}
}

func TestAnEmptySearchIsNotAFallback(t *testing.T) {
	// "Nothing matches" is a correct and often expected answer. Treating every
	// empty result as a failure would punish the tools for being asked honest
	// questions.
	for _, response := range []interface{}{
		protocol.GrepResponse{Total: 0},
		protocol.FindResponse{Total: 0},
	} {
		if outcome, found := ClassifyResponse(response); found || outcome != OK {
			t.Fatalf("%T: expected an empty result not to count as a fallback, got %q", response, outcome)
		}
	}
}

func TestRecordOutcomeStoresTheGivenClass(t *testing.T) {
	root := t.TempDir()
	recorder := New(root)
	recorder.RecordOutcome("arno.read_symbol", time.Millisecond, 20, NotFound)

	summary, err := recorder.Summarize()
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if summary.TotalErrors != 1 {
		t.Fatalf("expected the in-band failure counted, got %+v", summary)
	}
	if len(summary.Fallbacks) != 1 || summary.Fallbacks[0].Outcome != NotFound {
		t.Fatalf("expected a not_found fallback, got %+v", summary.Fallbacks)
	}
}

func TestRecordOutcomeDefaultsAnEmptyClassToOK(t *testing.T) {
	root := t.TempDir()
	recorder := New(root)
	recorder.RecordOutcome("arno.find", time.Millisecond, 20, "")

	summary, _ := recorder.Summarize()
	if summary.TotalErrors != 0 {
		t.Fatalf("expected an unset outcome to count as success, got %+v", summary)
	}
	if summary.TotalCalls != 1 {
		t.Fatalf("expected the call still recorded, got %d", summary.TotalCalls)
	}
}

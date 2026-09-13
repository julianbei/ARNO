package diagnostics

import (
	"strings"
	"testing"
)

// The failure output from a benchmark run: the assertion line alone left the
// agent calling job_output to learn what the test expected and got.
func TestDecisiveSummaryKeepsTheBlockAnAssertionIntroduces(t *testing.T) {
	output := strings.Join([]string{
		"=== RUN   TestPluginWithVersion",
		"--- FAIL: TestPluginWithVersion (0.00s)",
		"    command_test.go:76: Expected to contain: ",
		"         kubectl plugin version 1.0.0",
		"        Got:",
		"         Usage:",
		"          kubectl plugin [flags]",
		"FAIL",
		"FAIL\tgithub.com/spf13/cobra\t0.395s",
	}, "\n")

	summary := DecisiveSummary(output)
	for _, want := range []string{"Expected to contain:", "kubectl plugin version 1.0.0", "Got:", "Usage:"} {
		if !strings.Contains(summary, want) {
			t.Errorf("expected %q in the summary, got:\n%s", want, summary)
		}
	}
}

// A block is bounded: it stops at the next decisive line and never grows past
// the byte cap.
func TestContinuationStopsAtTheNextFailureAndIsCapped(t *testing.T) {
	lines := []string{"a_test.go:1: want:", "one", "--- FAIL: TestB", "two"}
	if got := withContinuation(lines, 0); got != "a_test.go:1: want: one" {
		t.Errorf("expected the block to stop at the next failure, got %q", got)
	}

	long := []string{"x_test.go:1: diff:", strings.Repeat("y", 1000)}
	if got := withContinuation(long, 0); len(got) > maxContinuationBytes+len("…") {
		t.Errorf("expected the block capped, got %d bytes", len(got))
	}

	if got := withContinuation([]string{"main.go:3:1: undefined: X", "next"}, 0); got != "main.go:3:1: undefined: X" {
		t.Errorf("a line that introduces no block stays alone, got %q", got)
	}
}

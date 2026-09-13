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

// The cobra benchmark output: the failing test is named first, then passing
// tests print thirteen expected `Error:` messages. The summary must name the
// failure, not the noise.
func TestDecisiveSummaryNamesTheFailingTestOverExpectedErrors(t *testing.T) {
	lines := []string{
		"[Debug] [Error] Error while parsing flags from args [--localroot value]: unknown flag: --localroot",
		"--- FAIL: TestCompleteWithDisableFlagParsing (0.00s)",
		`    completions_test.go:2635: expected: "--persistent\n-p\n--help", got: "--persistent\n-p"`,
	}
	for i := 0; i < 13; i++ {
		lines = append(lines, "Error: at least one of the flags in the group [a b] is required", "Usage:", "  testcmd [flags]", "")
	}
	lines = append(lines, "FAIL", "FAIL\tgithub.com/spf13/cobra\t0.267s", "FAIL", "exit status 1")

	summary := DecisiveSummary(strings.Join(lines, "\n"))
	for _, want := range []string{"--- FAIL: TestCompleteWithDisableFlagParsing", "completions_test.go:2635", "FAIL\tgithub.com/spf13/cobra"} {
		if !strings.Contains(summary, want) {
			t.Errorf("expected %q in the summary, got:\n%s", want, summary)
		}
	}
	if strings.Contains(summary, "flags in the group") {
		t.Errorf("expected errors printed by passing tests must not crowd out the failure:\n%s", summary)
	}
}

// Other runners name failures their own way.
func TestDecisiveSummaryRecognisesOtherRunnersFailures(t *testing.T) {
	cases := map[string]string{
		"pytest": "collected 3 items\nERROR log from a passing test\nFAILED tests/test_utils.py::test_parse - AssertionError: 1 != 2\n1 failed, 2 passed",
		"cargo":  "running 2 tests\ntest parser::handles_empty ... FAILED\nerror: test failed, to rerun pass `--lib`",
	}
	want := map[string]string{
		"pytest": "FAILED tests/test_utils.py::test_parse",
		"cargo":  "test parser::handles_empty ... FAILED",
	}
	for runner, output := range cases {
		if summary := DecisiveSummary(output); !strings.HasPrefix(summary, want[runner]) {
			t.Errorf("%s: expected the summary to lead with the failing test, got:\n%s", runner, summary)
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

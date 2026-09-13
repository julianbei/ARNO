package diagnostics

import (
	"strings"
	"testing"
)

// avaFailureOutput is ava 6's real output for one failed assertion, one
// rejected promise and one pass.
const avaFailureOutput = `  ✘ [fail]: probe assertion differs
  ✔ probe passes
  ✘ [fail]: probe rejected promise Rejected promise returned by test
  ─

  probe assertion differs

  test/zz-jade-probe.ts:4

   3: test('probe assertion differs', t => {
   4:   t.is('actual-value', 'expected-value');
   5: });

  Difference (- actual, + expected):

  - 'actual-value'
  + 'expected-value'

  › <anonymous> (test/zz-jade-probe.ts:4:4)



  probe rejected promise

  Rejected promise returned by test. Reason:

  TypeError {
    message: 'Illegal invocation',
  }

  TypeError: Illegal invocation
      at <anonymous> (/repo/test/zz-jade-probe.ts:8:8)
      at Test.callFn (file:///repo/node_modules/ava/lib/test.js:525:26)

  ─

  2 tests failed`

func TestJavaScriptFailureSummaryKeepsTheReason(t *testing.T) {
	trimmed := []string{}
	for _, line := range strings.Split(avaFailureOutput, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			trimmed = append(trimmed, line)
		}
	}
	untrimmed := strings.Split(avaFailureOutput, "\n")
	for i := range untrimmed {
		untrimmed[i] = strings.TrimSpace(untrimmed[i])
	}

	for name, lines := range map[string][]string{"blank lines kept": untrimmed, "blank lines dropped": trimmed} {
		got := strings.Join(testFailureLines(lines), "\n")
		for _, want := range []string{
			"✘ [fail]: probe assertion differs",
			"Difference (- actual, + expected): - 'actual-value' + 'expected-value'",
			"TypeError: Illegal invocation",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("%s: missing %q in:\n%s", name, want, got)
			}
		}
		for _, noise := range []string{"t.is(", "at Test.callFn", "zz-jade-probe.ts:4\n"} {
			if strings.Contains(got, noise) {
				t.Errorf("%s: code frame or stack %q kept in:\n%s", name, noise, got)
			}
		}
		if strings.Contains(got, "'actual-value' + 'expected-value' Rejected promise") {
			t.Errorf("%s: one test's detail ran into the next:\n%s", name, got)
		}
	}
}

func TestJestFailureSummaryKeepsTheReason(t *testing.T) {
	lines := []string{
		"FAIL src/sum.test.js",
		"● sum › adds numbers",
		"expect(received).toBe(expected) // Object.is equality",
		"Expected: 4",
		"Received: 5",
		"> 3 |   expect(sum(2, 2)).toBe(4);",
		"at Object.<anonymous> (src/sum.test.js:3:21)",
		"Tests:       1 failed, 1 total",
	}
	got := strings.Join(testFailureLines(lines), "\n")
	if !strings.Contains(got, "Expected: 4 Received: 5") || strings.Contains(got, "at Object.") {
		t.Errorf("expected jest's expected/received without the stack, got:\n%s", got)
	}
}
func TestAvaFailureSummaryKeepsTheMessageAfterALongErrorObject(t *testing.T) {
	lines := []string{
		"✘ [fail]: custom method Rejected promise returned by test",
		"─",
		"custom method",
		"Rejected promise returned by test. Reason:",
		"HTTPError {",
	}
	for i := 0; i < 20; i++ {
		lines = append(lines, "options: { retry: { afterStatusCodes: Array [ … ], backoffLimit: Infinity }, signal: AbortSignal {} },")
	}
	lines = append(lines, "}", "HTTPError: Request failed with status code 400 Bad Request: PURGE http://localhost/", "at <anonymous> (test/main.ts:9:9)", "─", "1 test failed")

	got := strings.Join(testFailureLines(lines), "\n")
	if !strings.Contains(got, "status code 400 Bad Request") {
		t.Errorf("the error message was cut behind the object dump:\n%s", got)
	}
}
func TestDiagnosticProvidersAreAskedInOrder(t *testing.T) {
	if got := strings.Join(DiagnosticProviderIDs(), ", "); got != "go parser and gopls, data file syntax, language server" {
		t.Fatalf("diagnostics registry order: got %s", got)
	}
}

package jobs

import (
	"strings"
	"testing"
)

func TestSummarizeCargoFindsThePanicInTheFailuresSection(t *testing.T) {
	raw := strings.Join([]string{
		"running 2 tests",
		"test standard::tests::ok_one ... ok",
		"test standard::tests::replacement_multi ... FAILED",
		"",
		"failures:",
		"",
		"---- standard::tests::replacement_multi stdout ----",
		"",
		"thread 'standard::tests::replacement_multi' panicked at crates/printer/src/util.rs:581:22:",
		"byte index 12 is out of bounds",
		"note: run with `RUST_BACKTRACE=1`",
		"",
		"failures:",
		"    standard::tests::replacement_multi",
		"",
		"test result: FAILED. 1 passed; 1 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.00s",
		"",
		"test result: ok. 0 passed; 0 failed; 2 ignored; 0 measured; 0 filtered out; finished in 0.00s",
	}, "\n")
	s := SummarizeTests(raw)
	if !s.Found || s.Passed != 1 || s.Failed != 1 || s.Skipped != 2 {
		t.Fatalf("counts: %+v", s)
	}
	if s.FirstFailure != "standard::tests::replacement_multi" {
		t.Fatalf("first failure: %q", s.FirstFailure)
	}
	if !strings.Contains(s.Detail, "util.rs:581:22") || !strings.Contains(s.Detail, "byte index 12") {
		t.Fatalf("detail: %q", s.Detail)
	}
}

func TestSummarizeGoWithAndWithoutVerbose(t *testing.T) {
	quiet := "--- FAIL: TestPlugin (0.00s)\n    command_test.go:42: got \"a\", want \"b\"\nFAIL\nFAIL\tgithub.com/spf13/cobra\t0.2s\n"
	verbose := "=== RUN   TestPlugin\n    command_test.go:42: got \"a\", want \"b\"\n--- FAIL: TestPlugin (0.00s)\n=== RUN   TestOther\n--- PASS: TestOther (0.00s)\nFAIL\n"
	for name, raw := range map[string]string{"quiet": quiet, "verbose": verbose} {
		s := SummarizeTests(raw)
		if !s.Found || s.Failed != 1 || s.FirstFailure != "TestPlugin" || !strings.Contains(s.Detail, "command_test.go:42") {
			t.Errorf("%s: %+v", name, s)
		}
	}
}

func TestSummarizePytestNamesTheEnvironment(t *testing.T) {
	raw := "FAILED tests/test_lowlevel.py::test_use_proxy[http_proxy-http] - requests.exceptions.InvalidSchema: Missing dependencies for SOCKS support.\n" +
		"===== 1 failed, 300 passed, 2 skipped, 3 warnings in 12.30s =====\n"
	s := SummarizeTests(raw)
	if s.Failed != 1 || s.Passed != 300 || s.Skipped != 2 {
		t.Fatalf("counts: %+v", s)
	}
	if s.FirstFailure != "tests/test_lowlevel.py::test_use_proxy[http_proxy-http]" || !strings.Contains(s.Detail, "InvalidSchema") {
		t.Fatalf("failure: %+v", s)
	}
	if line := s.FailureLine(""); !strings.Contains(line, "SOCKS support (PySocks) is not installed") {
		t.Fatalf("environment missing from %q", line)
	}
}

func TestSummarizeJestAndVitest(t *testing.T) {
	jest := "  ● download › keeps url after clone\n\n    expect(received).toBe(expected)\n\nTests:       1 failed, 12 passed, 13 total\n"
	s := SummarizeTests(jest)
	if s.Failed != 1 || s.Passed != 12 || s.FirstFailure != "download › keeps url after clone" || !strings.Contains(s.Detail, "toBe") {
		t.Fatalf("jest: %+v", s)
	}
	vitest := " FAIL  test/a.test.ts > clone > keeps url\nAssertionError: expected undefined to be 'x'\n Test Files  1 failed (1)\n      Tests  1 failed | 4 passed (5)\n"
	s = SummarizeTests(vitest)
	if s.Failed != 1 || s.Passed != 4 || !strings.Contains(s.FirstFailure, "keeps url") || !strings.Contains(s.Detail, "AssertionError") {
		t.Fatalf("vitest: %+v", s)
	}
}

func TestSummarizeAvaCountsKnownFailuresAsSkipped(t *testing.T) {
	raw := "  ✘ [fail]: browser › webkit - should copy origin response info\n" +
		"  browserType.launch: Executable doesn't exist at /cache/webkit\n" +
		"  ─\n\n  32 tests failed\n  2 known failures\n  596 tests passed\n"
	s := SummarizeTests(raw)
	if s.Failed != 32 || s.Skipped != 2 || s.Passed != 596 {
		t.Fatalf("counts: %+v", s)
	}
	if !strings.HasPrefix(s.FirstFailure, "browser › webkit") || len(s.Environment) != 1 {
		t.Fatalf("failure: %+v", s)
	}
}

func TestSummarizeUnknownOutputIsNotFound(t *testing.T) {
	if s := SummarizeTests("Build succeeded in 1.2s\n"); s.Found {
		t.Fatalf("build output read as tests: %+v", s)
	}
	if got := (TestSummary{Found: true}).Counts(); got != "0 tests ran" {
		t.Fatalf("empty counts: %q", got)
	}
}

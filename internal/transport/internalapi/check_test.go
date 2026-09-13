package internalapi

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/julianbei/jade/internal/jobs"
)

func TestNormalizeCheckKindAcceptsSynonyms(t *testing.T) {
	cases := map[string]string{
		"":          "build",
		"build":     "build",
		"typecheck": "typecheck",
		"vet":       "typecheck",
		// lint is its own kind now: Check runs the declared lint commands,
		// and falls back to typecheck only when none are declared.
		"lint":      "lint",
		"codegen":   "codegen",
		"generate":  "codegen",
		"tests":     "tests",
		"test":      "tests",
		"  Build  ": "build",
	}
	for input, want := range cases {
		got, err := normalizeCheckKind(input)
		if err != nil {
			t.Fatalf("normalizeCheckKind(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("normalizeCheckKind(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeCheckKindRejectsUnknown(t *testing.T) {
	// Substituting a different check and reporting success would be worse
	// than erroring — the caller would believe something was verified that
	// never ran.
	if _, err := normalizeCheckKind("deploy"); err == nil {
		t.Fatalf("expected an unknown kind to be rejected")
	}
}

func TestCheckTimeoutBounds(t *testing.T) {
	if got := checkTimeout(0); got != defaultCheckTimeout {
		t.Fatalf("expected the default for 0, got %v", got)
	}
	if got := checkTimeout(-5); got != defaultCheckTimeout {
		t.Fatalf("expected the default for a negative value, got %v", got)
	}
	if got := checkTimeout(10); got != 10*time.Second {
		t.Fatalf("expected 10s, got %v", got)
	}
	// A caller must not be able to pin the connection indefinitely.
	if got := checkTimeout(100000); got != maxCheckTimeout {
		t.Fatalf("expected the cap, got %v", got)
	}
}

func TestFailureMarkersAreASecondarySignal(t *testing.T) {
	// Some tools genuinely exit 0 while reporting a failure, so the text scan
	// stays — but only as a way to fail a job, never as a way to pass one.
	failing := []struct{ summary, raw string }{
		{"FAIL\tgithub.com/x/y\t0.1s", ""},
		{"", "main.go:9:3: undefined: Thing"},
		{"", "panic: runtime error"},
		{"", "cannot find package"},
		{"some error occurred", ""},
	}
	for _, c := range failing {
		if jobPassed(jobs.JobOutput{Summary: c.summary, Raw: c.raw}) {
			t.Fatalf("expected failure for summary=%q raw=%q", c.summary, c.raw)
		}
	}

	passing := []struct{ summary, raw string }{
		{"", ""},
		{"no output", ""},
		{"ok  \tgithub.com/x/y\t0.2s", "ok  \tgithub.com/x/y\t0.2s"},
	}
	for _, c := range passing {
		if !jobPassed(jobs.JobOutput{Summary: c.summary, Raw: c.raw}) {
			t.Fatalf("expected pass for summary=%q raw=%q", c.summary, c.raw)
		}
	}
}

func TestNonZeroExitFailsHoweverCheerfulTheOutput(t *testing.T) {
	// The bug this replaced: the verdict came from output text alone, so
	// `echo "everything looks fine"; exit 3` was reported as a pass. Exit
	// status is authoritative and no wording can override it.
	output := jobs.JobOutput{
		Status:  "completed",
		Summary: "everything looks fine",
		Raw:     "everything looks fine\nall green\nnothing to report",
		Failed:  true,
	}
	if jobPassed(output) {
		t.Fatalf("expected a non-zero exit to fail regardless of output")
	}
}
func TestSuccessSummaryCountsInsteadOfQuotingATail(t *testing.T) {
	// The whole point. An all-green run summarized to whichever three "ok"
	// lines sorted last, which is correct and reads as a partial run.
	var raw strings.Builder
	for i := 0; i < 42; i++ {
		fmt.Fprintf(&raw, "ok  \tgithub.com/x/pkg%02d\t0.2s\n", i)
	}

	got := successSummary(raw.String())
	if got != "42 packages ok" {
		t.Fatalf("expected a count, got %q", got)
	}
}

func TestSuccessSummaryReportsPackagesWithoutTests(t *testing.T) {
	raw := "ok  \tgithub.com/x/a\t0.2s\n?   \tgithub.com/x/b\t[no test files]\n?   \tgithub.com/x/c\t[no test files]\n"
	got := successSummary(raw)
	if got != "1 packages ok, 2 with no test files" {
		t.Fatalf("expected both counts, got %q", got)
	}
}

func TestSuccessSummaryShowsShortOutputVerbatim(t *testing.T) {
	// A one-line success message is worth more than a count of it.
	if got := successSummary("Build succeeded in 1.2s"); got != "Build succeeded in 1.2s" {
		t.Fatalf("expected the line kept, got %q", got)
	}
}

func TestSuccessSummaryOnSilentOutput(t *testing.T) {
	// go build and gofmt -l print nothing at all when clean.
	if got := successSummary("   \n\n"); got != "no output" {
		t.Fatalf("expected \"no output\", got %q", got)
	}
}

func TestSuccessSummaryCountsUnrecognizedLongOutput(t *testing.T) {
	// A sample of a log nobody asked for is the thing being removed, so
	// unrecognised output reports its size rather than a slice of itself.
	var raw strings.Builder
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&raw, "compiling module %d\n", i)
	}
	got := successSummary(raw.String())
	if got != "20 lines of output" {
		t.Fatalf("expected a line count, got %q", got)
	}
}

func TestVerdictSummaryKeepsTheDecisiveLineOnFailure(t *testing.T) {
	// Failure output is good as it is: the compiler diagnostic is exactly what
	// the caller needs, and counting it would destroy the only useful part.
	decisive := "main.go:42:3: undefined: Thing"
	got := verdictSummary(false, decisive, "lots\nof\nbuild\noutput\n"+decisive)
	if got != decisive {
		t.Fatalf("expected the decisive line preserved, got %q", got)
	}
}

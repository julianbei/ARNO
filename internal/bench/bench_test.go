package bench

import (
	"errors"
	"strings"
	"testing"
)

func constantArm(output string, calls int) Arm {
	return func() (string, int, error) { return output, calls, nil }
}

func failingArm(message string) Arm {
	return func() (string, int, error) { return "", 1, errors.New(message) }
}

func TestEstimateTokensRoundsUp(t *testing.T) {
	cases := map[int]int{0: 0, 1: 1, 4: 1, 5: 2, 8: 2, 9: 3}
	for bytes, want := range cases {
		if got := EstimateTokens(bytes); got != want {
			t.Fatalf("EstimateTokens(%d) = %d, want %d", bytes, got, want)
		}
	}
	if EstimateTokens(-10) != 0 {
		t.Fatalf("expected negative input to yield 0")
	}
}

func TestRunMeasuresBothArms(t *testing.T) {
	results := Run([]Scenario{{
		Name:     "example",
		Question: "?",
		Jade:     constantArm(strings.Repeat("j", 400), 1),
		Shell:    constantArm(strings.Repeat("s", 100), 2),
	}})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Jade.Bytes != 400 || r.Shell.Bytes != 100 {
		t.Fatalf("byte counts wrong: %+v", r)
	}
	if r.Jade.Calls != 1 || r.Shell.Calls != 2 {
		t.Fatalf("call counts wrong: %+v", r)
	}
	if ratio := r.TokenRatio(); ratio != 4 {
		t.Fatalf("expected a 4x ratio, got %v", ratio)
	}
}

func TestTokenRatioBelowOneMeansJadeIsCheaper(t *testing.T) {
	r := Result{
		Jade:  ArmResult{Tokens: 25},
		Shell: ArmResult{Tokens: 100},
	}
	if ratio := r.TokenRatio(); ratio != 0.25 {
		t.Fatalf("expected 0.25, got %v", ratio)
	}
}

func TestTokenRatioHandlesEmptyShellOutput(t *testing.T) {
	r := Result{Jade: ArmResult{Tokens: 10}, Shell: ArmResult{Tokens: 0}}
	if ratio := r.TokenRatio(); ratio != 0 {
		t.Fatalf("expected 0 for an unmeasurable ratio, got %v", ratio)
	}
}

func TestRunRecordsArmFailuresInsteadOfAborting(t *testing.T) {
	// One broken scenario must not hide the rest of the suite.
	results := Run([]Scenario{
		{Name: "broken", Jade: failingArm("boom"), Shell: constantArm("ok", 1)},
		{Name: "fine", Jade: constantArm("abcd", 1), Shell: constantArm("abcd", 1)},
	})

	if len(results) != 2 {
		t.Fatalf("expected both scenarios measured, got %d", len(results))
	}
	if results[0].Jade.Err == nil {
		t.Fatalf("expected the failure to be recorded")
	}
	if results[1].Jade.Tokens != 1 {
		t.Fatalf("expected the second scenario to still be measured, got %+v", results[1])
	}
}

func TestRunRecordsMissingArm(t *testing.T) {
	results := Run([]Scenario{{Name: "no jade arm", Shell: constantArm("x", 1)}})
	if results[0].Jade.Err == nil {
		t.Fatalf("expected a nil arm to be reported as an error, not counted as 0 tokens")
	}
}

func TestReportShowsRatiosTotalsAndCaveats(t *testing.T) {
	report := Report(Run([]Scenario{{
		Name:  "example",
		Jade:  constantArm(strings.Repeat("j", 400), 1),
		Shell: constantArm(strings.Repeat("s", 100), 1),
	}}))

	if !strings.Contains(report, "4.00x") {
		t.Fatalf("expected the ratio in the report, got:\n%s", report)
	}
	if !strings.Contains(report, "TOTAL") {
		t.Fatalf("expected a total row, got:\n%s", report)
	}
	// The caveat must ship with the numbers. A token-only measurement
	// presented as a verdict on jade overall would misrepresent it.
	if !strings.Contains(report, "NOT measured") {
		t.Fatalf("expected the report to disclose what it does not measure")
	}
}

func TestReportListsErrors(t *testing.T) {
	report := Report(Run([]Scenario{{
		Name:  "broken",
		Jade:  failingArm("boom"),
		Shell: constantArm("x", 1),
	}}))

	if !strings.Contains(report, "ERR") {
		t.Fatalf("expected a failed arm marked in the table, got:\n%s", report)
	}
	if !strings.Contains(report, "boom") {
		t.Fatalf("expected the error detail listed, got:\n%s", report)
	}
}

package jobs

import (
	"testing"

	"github.com/julianbei/jade/internal/protocol"
)

func TestOutcomeClassifiesEveryWayAJobEnds(t *testing.T) {
	cases := []struct {
		name     string
		output   JobOutput
		finished bool
		want     protocol.ValidationOutcome
	}{
		{"wait ran out", JobOutput{Status: "running"}, false, protocol.OutcomeTimedOut},
		{"killed for its own timeout", JobOutput{Raw: "partial\ntimed out after 1s and was killed", Failed: true}, true, protocol.OutcomeTimedOut},
		{"shell cannot find the tool", JobOutput{Raw: "sh: rubocop: command not found\nexit status 127", Failed: true}, true, protocol.OutcomeUnavailable},
		{"binary cannot start", JobOutput{Raw: `exec: "cargo": executable file not found in $PATH`, Failed: true}, true, protocol.OutcomeUnavailable},
		{"nothing discovered", JobOutput{Raw: `no validation command configured for kind "build"`}, true, protocol.OutcomeUnavailable},
		{"non-zero exit with calm output", JobOutput{Raw: "everything looks fine\nexit status 3", Failed: true}, true, protocol.OutcomeFailed},
		{"exit zero but reports failure", JobOutput{Raw: "--- FAIL: TestX"}, true, protocol.OutcomeFailed},
		{"clean", JobOutput{Raw: "ok  \tpkg\t0.1s"}, true, protocol.OutcomePassed},
		{"no output at all", JobOutput{}, true, protocol.OutcomePassed},
	}
	for _, tc := range cases {
		if got := Outcome(tc.output, tc.finished); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

package internalapi

import (
	"strings"
	"testing"
	"time"

	"github.com/julianbei/arno/internal/jobs"
)

// TestRunTestsWaitReturnsARealVerdictInOneCall is the point of 12.3: the
// caller gets pass/fail from a single call instead of a job ID plus a poll.
// It runs a real `go test` against a leaf package with no test files, which
// completes fast and cannot recurse into this suite.
func TestRunTestsWaitReturnsARealVerdictInOneCall(t *testing.T) {
	runner := jobs.NewRunner(nil)
	id := runner.Start("tests")
	runner.RunCommand(id, repoRoot(t), "go", "test", "./internal/protocol/")

	output, finished := runner.Wait(id, 60*time.Second)
	if !finished {
		t.Fatalf("expected the run to finish within the timeout")
	}
	if output.Status != "completed" {
		t.Fatalf("expected completed, got %q", output.Status)
	}
	if !jobPassed(output) {
		t.Fatalf("expected a clean package to pass, got summary=%q raw=%q", output.Summary, output.Raw)
	}
}

func TestRunTestsWaitReportsFailureAsFailure(t *testing.T) {
	runner := jobs.NewRunner(nil)
	id := runner.Start("tests")
	// A package path that does not exist makes the real command fail.
	runner.RunCommand(id, repoRoot(t), "go", "test", "./internal/does-not-exist/")

	output, finished := runner.Wait(id, 60*time.Second)
	if !finished {
		t.Fatalf("expected the run to finish")
	}
	// The verdict must come from the command's own result, not from the job
	// merely having completed — a failing run also "completes".
	if jobPassed(output) {
		t.Fatalf("expected a failing run to be reported as failing, got summary=%q", output.Summary)
	}
}

func TestRunTestsTimeoutDoesNotReadAsPass(t *testing.T) {
	// The property that makes waiting safe: a run that has not finished must
	// never be reported as a pass.
	runner := jobs.NewRunner(nil)
	id := runner.Start("tests")

	output, finished := runner.Wait(id, 20*time.Millisecond)
	if finished {
		t.Fatalf("expected the wait to time out")
	}
	if output.Status == "completed" {
		t.Fatalf("expected the job to still be running, got %q", output.Status)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	// This package sits at internal/transport/internalapi.
	return "../../.."
}

func TestCheckTimeoutIsSharedWithRunTests(t *testing.T) {
	// 12.3 reuses 12.2's bound rather than declaring its own, so the two
	// tools cannot drift in how long they will block.
	if checkTimeout(0) != defaultCheckTimeout {
		t.Fatalf("expected run_tests to inherit the shared default")
	}
	if !strings.Contains(defaultCheckTimeout.String(), "1m30s") {
		t.Fatalf("unexpected shared default %v", defaultCheckTimeout)
	}
}

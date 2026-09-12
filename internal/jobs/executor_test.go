package jobs

import (
	"strings"
	"testing"
	"time"
)

func waitForCompletion(t *testing.T, r *Runner, id string) (status string, summary string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, status, summary, ok := r.Status(id)
		if !ok {
			t.Fatalf("unknown job id %s", id)
		}
		if status == "completed" {
			return status, summary
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s did not complete within timeout", id)
	return "", ""
}

func TestRunCommandActuallyExecutesAndCompletesTheJob(t *testing.T) {
	r := NewRunner(nil)
	id := r.Start("build")

	r.RunCommand(id, ".", "go", "version")

	status, _ := waitForCompletion(t, r, id)
	if status != "completed" {
		t.Fatalf("expected status completed, got %s", status)
	}

	output, ok := r.Output(id)
	if !ok {
		t.Fatalf("expected job output to be present")
	}
	if !strings.Contains(output.Raw, "go version") {
		t.Fatalf("expected output.Raw output to contain real command output, got %q", output.Raw)
	}
}

func TestRunCommandCapturesFailureOutput(t *testing.T) {
	r := NewRunner(nil)
	id := r.Start("typecheck")

	r.RunCommand(id, ".", "go", "not-a-real-subcommand")

	waitForCompletion(t, r, id)

	output, ok := r.Output(id)
	if !ok {
		t.Fatalf("expected job output to be present")
	}
	if output.Raw == "" {
		t.Fatalf("expected non-empty output for a failing command")
	}
}

func TestRunValidationCommandUsesMappedArgsForKnownKind(t *testing.T) {
	r := NewRunner(nil)
	id := r.Start("build")

	r.RunValidationCommand(id, "../..", "build")

	status, _ := waitForCompletion(t, r, id)
	if status != "completed" {
		t.Fatalf("expected status completed, got %s", status)
	}
}

func TestRunCommandKillsWholeProcessGroupOnTimeout(t *testing.T) {
	r := NewRunner(nil)
	id := r.Start("build")

	// The shell exits almost immediately, backgrounding a child that keeps
	// the output pipe open for 3s. Killing only the direct child (the shell)
	// would leave that grandchild running and holding the pipe open, so
	// completion would not actually happen until the full 3s elapses —
	// exactly the bug droneship's own notes describe. A correct process-group
	// kill kills the grandchild too, so this should complete shortly after
	// the 150ms timeout, not after 3s.
	start := time.Now()
	r.RunCommandWithTimeout(id, ".", 150*time.Millisecond, "sh", "-c", "sleep 3 & exit 0")

	status, _ := waitForCompletion(t, r, id)
	elapsed := time.Since(start)

	if status != "completed" {
		t.Fatalf("expected status completed, got %s", status)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("expected timeout to kill the whole process group and complete quickly, took %s", elapsed)
	}

	output, ok := r.Output(id)
	if !ok {
		t.Fatalf("expected job output to be present")
	}
	if !strings.Contains(output.Raw, "timed out") {
		t.Fatalf("expected output to note the timeout, got %q", output.Raw)
	}
}

func TestRunValidationCommandFailsFastForUnknownKind(t *testing.T) {
	r := NewRunner(nil)
	id := r.Start("mystery")

	r.RunValidationCommand(id, ".", "mystery")

	status, summary := waitForCompletion(t, r, id)
	if status != "completed" {
		t.Fatalf("expected status completed, got %s", status)
	}
	if summary == "" {
		output, _ := r.Output(id)
		if !strings.Contains(output.Raw, "no validation command configured") {
			t.Fatalf("expected explanatory output for unknown kind, got %q", output.Raw)
		}
	}
}

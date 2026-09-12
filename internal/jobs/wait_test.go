package jobs

import (
	"testing"
	"time"
)

func TestWaitReturnsWhenTheJobCompletes(t *testing.T) {
	r := NewRunner(nil)
	id := r.Start("build")

	go func() {
		time.Sleep(20 * time.Millisecond)
		r.CompleteWithOutput(id, "ok  \tinternal/code\t0.2s")
	}()

	output, finished := r.Wait(id, 2*time.Second)
	if !finished {
		t.Fatalf("expected the wait to observe completion")
	}
	if output.Status != "completed" {
		t.Fatalf("expected completed, got %q", output.Status)
	}
	if output.Summary == "" {
		t.Fatalf("expected the summary to be available to the waiter")
	}
}

func TestWaitTimesOutWithoutClaimingCompletion(t *testing.T) {
	r := NewRunner(nil)
	id := r.Start("tests")

	// "Still running" and "finished with no output" are different answers
	// and must not collapse into one.
	output, finished := r.Wait(id, 30*time.Millisecond)
	if finished {
		t.Fatalf("expected finished=false for a job that never completed")
	}
	if output.Status == "completed" {
		t.Fatalf("expected the job to still be running, got %q", output.Status)
	}
}

func TestWaitReturnsImmediatelyForAlreadyCompletedJob(t *testing.T) {
	r := NewRunner(nil)
	id := r.Start("build")
	r.CompleteWithOutput(id, "done")

	start := time.Now()
	output, finished := r.Wait(id, 2*time.Second)
	if !finished || output.Status != "completed" {
		t.Fatalf("expected an already-completed job to return at once, got %+v", output)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("expected no blocking for a finished job, waited %v", elapsed)
	}
}

func TestWaitOnUnknownJobReportsNotFinished(t *testing.T) {
	r := NewRunner(nil)
	if _, finished := r.Wait("job-does-not-exist", 50*time.Millisecond); finished {
		t.Fatalf("expected an unknown job to report not finished")
	}
}

func TestCompleteTwiceDoesNotPanic(t *testing.T) {
	// Closing an already-closed channel panics; the guard in
	// CompleteWithOutput has to hold.
	r := NewRunner(nil)
	id := r.Start("build")
	r.CompleteWithOutput(id, "first")
	r.CompleteWithOutput(id, "second")

	if _, finished := r.Wait(id, time.Second); !finished {
		t.Fatalf("expected the job to still read as finished")
	}
}

func TestMultipleWaitersAreAllReleased(t *testing.T) {
	r := NewRunner(nil)
	id := r.Start("tests")

	results := make(chan bool, 3)
	for i := 0; i < 3; i++ {
		go func() {
			_, finished := r.Wait(id, 2*time.Second)
			results <- finished
		}()
	}

	time.Sleep(20 * time.Millisecond)
	r.CompleteWithOutput(id, "ok")

	for i := 0; i < 3; i++ {
		select {
		case finished := <-results:
			if !finished {
				t.Fatalf("expected every waiter released on completion")
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("a waiter was never released")
		}
	}
}

package jobs

import (
	"sync"
	"time"

	"github.com/julianbei/jade/internal/diagnostics"
	"github.com/julianbei/jade/internal/events"
)

// Runner tracks asynchronous validation jobs.
type Runner struct {
	mu      sync.Mutex
	next    int
	kind    map[string]string
	status  map[string]string
	summary map[string]string
	raw     map[string]string
	omitted map[string]int
	// full is each job's output as kept for paging through job_output, bounded
	// far above raw, which verdicts read.
	full map[string]string
	// failed records that a job's process exited non-zero.
	//
	// Kept separate from the output text because the verdict was previously
	// derived from that text alone — scanning for "fail", "error", "panic:" —
	// which meant any command that exited non-zero while printing something
	// cheerful was reported as passing. Demonstrated with
	// `echo "everything looks fine"; exit 3`, which returned `pass`. That is
	// tolerable for Go's build/vet/test, which announce their own failures in
	// words, and simply wrong for the arbitrary project commands the repo
	// command registry exists to run: lint, codegen and migrate signal by exit
	// code.
	failed map[string]bool
	// done carries one channel per job, closed when the job completes, so
	// waiting is a block rather than a poll loop.
	done map[string]chan struct{}
	// running maps a run's key to the job still executing it, so the same
	// run asked for again joins that job instead of starting a second one.
	running map[string]string
	bus     *events.Bus
}

func NewRunner(bus *events.Bus) *Runner {
	return &Runner{
		next:    1,
		kind:    make(map[string]string),
		status:  make(map[string]string),
		summary: make(map[string]string),
		raw:     make(map[string]string),
		full:    make(map[string]string),
		omitted: make(map[string]int),
		failed:  make(map[string]bool),
		done:    make(map[string]chan struct{}),
		running: make(map[string]string),
		bus:     bus,
	}
}

func (r *Runner) Start(kind string) string {
	r.mu.Lock()
	id := "job-" + itoa(r.next)
	r.next++
	r.kind[id] = kind
	r.status[id] = "running"
	r.done[id] = make(chan struct{})
	r.mu.Unlock()

	if r.bus != nil {
		r.bus.Publish(events.Event{
			Type:   "JOB_STARTED",
			Entity: "job",
			Payload: map[string]string{
				"id":   id,
				"kind": kind,
			},
		})
	}

	return id
}

// StartOnce starts a job for key unless one is still running for it, in which
// case it returns that job and joined is true; the caller then waits on it
// instead of running the command again.
//
// A test run that outlived its wait used to be started twice more by an agent
// that could not poll it — about eight minutes of duplicated cargo test in
// one benchmark run. Keys carry the workspace revision, so a run asked for
// after an edit never joins one that started before it.
func (r *Runner) StartOnce(kind string, key string) (id string, joined bool) {
	r.mu.Lock()
	if existing, ok := r.running[key]; ok && key != "" {
		r.mu.Unlock()
		return existing, true
	}
	r.mu.Unlock()

	id = r.Start(kind)
	if key != "" {
		r.mu.Lock()
		if r.status[id] == "running" {
			r.running[key] = id
		}
		r.mu.Unlock()
	}
	return id, false
}

// ValidationKey identifies a discovered validation run, shared by check and
// by apply's post-edit check so a check called after apply joins its run.
func ValidationKey(dir string, kind string, revision string) string {
	return "validate|" + dir + "|" + kind + "|" + revision
}

func (r *Runner) Complete(id string, summary string) {
	r.CompleteWithOutput(id, summary)
}

// CompleteWithOutput marks a job complete with no exit-status information.
// Callers that ran a real process should use CompleteWithResult instead, so
// the verdict does not have to be guessed from the output text.
func (r *Runner) CompleteWithOutput(id string, output string) {
	r.CompleteWithResult(id, output, false)
}

// CompleteWithResult marks a job complete and records whether its process
// failed. failed is authoritative: a non-zero exit is a failure regardless of
// how reassuring the output reads.
func (r *Runner) CompleteWithResult(id string, output string, failed bool) {
	// The summary is computed from the FULL output, before clamping. The
	// decisive line of a long failure is frequently in the middle — exactly
	// the part the clamp drops — so summarizing the clamped text would let
	// the size bound silently degrade the answer jade is best at giving.
	summary := diagnostics.DecisiveSummary(output)
	clamped, omitted := clampRawOutput(output)

	r.mu.Lock()
	r.status[id] = "completed"
	r.summary[id] = summary
	r.raw[id] = clamped
	r.full[id] = storedRawOutput(output)
	r.omitted[id] = omitted
	r.failed[id] = failed
	for key, running := range r.running {
		if running == id {
			delete(r.running, key)
		}
	}
	kind := r.kind[id]
	done := r.done[id]
	r.mu.Unlock()

	// Closing rather than sending: every waiter is released, and a job that
	// nobody waited on costs nothing.
	if done != nil {
		select {
		case <-done:
			// Already closed — Complete called twice. Nothing to do.
		default:
			close(done)
		}
	}

	if r.bus != nil {
		r.bus.Publish(events.Event{
			Type:   "JOB_COMPLETED",
			Entity: "job",
			Payload: map[string]string{
				"id":      id,
				"kind":    kind,
				"summary": summary,
			},
		})
	}
}

func (r *Runner) Status(id string) (string, string, string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	status, ok := r.status[id]
	if !ok {
		return "", "", "", false
	}
	return r.kind[id], status, r.summary[id], true
}

// JobOutput is a completed job's stored result. It replaced a five-value
// positional return once 8.1 added a sixth field — at that width, callers
// start mixing up same-typed positions.
type JobOutput struct {
	Kind    string
	Status  string
	Summary string
	Raw     string
	// Failed reports that the job's process exited non-zero. It is the
	// authoritative verdict; output text is only a secondary signal, for tools
	// that exit 0 while reporting a failure.
	Failed bool
	// OmittedBytes is how much of the raw output the size clamp dropped, 0
	// when nothing was cut. Callers should surface it: silently truncated
	// output is worse than visibly truncated output, because a reader who
	// cannot see that bytes are missing will read the remainder as complete.
	OmittedBytes int
}

func (r *Runner) Output(id string) (JobOutput, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	status, ok := r.status[id]
	if !ok {
		return JobOutput{}, false
	}
	return JobOutput{
		Kind:         r.kind[id],
		Status:       status,
		Summary:      r.summary[id],
		Raw:          r.raw[id],
		Failed:       r.failed[id],
		OmittedBytes: r.omitted[id],
	}, true
}

// FullOutput is a completed job's output as kept for paging: whole unless it
// passed maxStoredOutputBytes. Output keeps the clamped form verdicts read.
func (r *Runner) FullOutput(id string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	full, ok := r.full[id]
	return full, ok
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}

	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}

// Wait blocks until job id completes or timeout elapses. The bool reports
// whether the job finished: false means it is still running, which is a
// different answer from "finished with no output" and callers must not
// collapse the two.
//
// This exists because an agent asking "does it build" wants the answer, not
// a job ID to poll. The async model is right for long runs; making every
// short run cost two calls plus a poll is why the native shell command kept
// winning in the dogfood friction log.
func (r *Runner) Wait(id string, timeout time.Duration) (JobOutput, bool) {
	r.mu.Lock()
	done, known := r.done[id]
	r.mu.Unlock()

	if !known {
		// Unknown job, or one that completed before any channel existed.
		output, ok := r.Output(id)
		return output, ok && output.Status == "completed"
	}

	select {
	case <-done:
		output, ok := r.Output(id)
		return output, ok
	case <-time.After(timeout):
		output, _ := r.Output(id)
		return output, false
	}
}

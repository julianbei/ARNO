package jobs

import (
	"sync"

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
	bus     *events.Bus
}

func NewRunner(bus *events.Bus) *Runner {
	return &Runner{
		next:    1,
		kind:    make(map[string]string),
		status:  make(map[string]string),
		summary: make(map[string]string),
		raw:     make(map[string]string),
		bus:     bus,
	}
}

func (r *Runner) Start(kind string) string {
	r.mu.Lock()
	id := "job-" + itoa(r.next)
	r.next++
	r.kind[id] = kind
	r.status[id] = "running"
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

func (r *Runner) Complete(id string, summary string) {
	r.CompleteWithOutput(id, summary)
}

func (r *Runner) CompleteWithOutput(id string, output string) {
	summary := diagnostics.DecisiveSummary(output)

	r.mu.Lock()
	r.status[id] = "completed"
	r.summary[id] = summary
	r.raw[id] = output
	kind := r.kind[id]
	r.mu.Unlock()

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

func (r *Runner) Output(id string) (string, string, string, string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	status, ok := r.status[id]
	if !ok {
		return "", "", "", "", false
	}
	return r.kind[id], status, r.summary[id], r.raw[id], true
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

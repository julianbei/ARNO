package jobs

import (
	"sync"

	"github.com/julianbei/jade/internal/events"
)

// Runner tracks asynchronous validation jobs.
type Runner struct {
	mu      sync.Mutex
	next    int
	status  map[string]string
	summary map[string]string
	bus     *events.Bus
}

func NewRunner(bus *events.Bus) *Runner {
	return &Runner{
		next:    1,
		status:  make(map[string]string),
		summary: make(map[string]string),
		bus:     bus,
	}
}

func (r *Runner) Start(kind string) string {
	r.mu.Lock()
	id := "job-" + itoa(r.next)
	r.next++
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
	r.mu.Lock()
	r.status[id] = "completed"
	r.summary[id] = summary
	r.mu.Unlock()

	if r.bus != nil {
		r.bus.Publish(events.Event{
			Type:   "JOB_COMPLETED",
			Entity: "job",
			Payload: map[string]string{
				"id":      id,
				"summary": summary,
			},
		})
	}
}

func (r *Runner) Status(id string) (string, string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	status, ok := r.status[id]
	if !ok {
		return "", "", false
	}
	return status, r.summary[id], true
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

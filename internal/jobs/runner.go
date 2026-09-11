package jobs

import "sync"

// Runner tracks asynchronous validation jobs.
type Runner struct {
	mu   sync.Mutex
	next int
}

func NewRunner() *Runner {
	return &Runner{next: 1}
}

func (r *Runner) Start(kind string) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	id := "job-" + itoa(r.next)
	r.next++
	return id
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

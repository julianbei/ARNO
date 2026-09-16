package main

import (
	"bufio"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/julianbei/arno/internal/events"
)

// maxUnclaimedCompletions bounds the completions kept for jobs whose call has
// not yet recorded them as backgrounded.
const maxUnclaimedCompletions = 64

// backgroundJobs are the jobs calls started with wait: false, whose completion
// this session announces. A job can finish before its call returns and
// records it, so recent completions nobody waits for are kept briefly.
type backgroundJobs struct {
	mu       sync.Mutex
	waiting  map[string]bool
	finished map[string]events.Event
	order    []string
}

// add records id as backgrounded. It returns the completion when the job has
// already finished, which is then announced at once.
func (b *backgroundJobs) add(id string) (events.Event, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if event, done := b.finished[id]; done {
		delete(b.finished, id)
		return event, true
	}
	if b.waiting == nil {
		b.waiting = map[string]bool{}
	}
	b.waiting[id] = true
	return events.Event{}, false
}

// finish records a completion and reports whether a call backgrounded it.
func (b *backgroundJobs) finish(event events.Event) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := event.Payload["id"]
	if b.waiting[id] {
		delete(b.waiting, id)
		return true
	}
	if b.finished == nil {
		b.finished = map[string]events.Event{}
	}
	b.finished[id] = event
	b.order = append(b.order, id)
	for len(b.order) > maxUnclaimedCompletions {
		delete(b.finished, b.order[0])
		b.order = b.order[1:]
	}
	return false
}

var jobIDPattern = regexp.MustCompile(`\bjob-\d+\b`)

// rememberBackgroundJobs records the jobs a call started with wait: false, so
// their completion is announced instead of polled for. A waited call's
// response already carries the verdict and is not announced.
func (s *mcpServer) rememberBackgroundJobs(args map[string]interface{}, result mcpToolResult) {
	if wait, given := args["wait"].(bool); !given || wait {
		return
	}
	for _, content := range result.Content {
		for _, id := range jobIDPattern.FindAllString(content.Text, -1) {
			if event, done := s.background.add(id); done {
				s.announceJob(event)
			}
		}
	}
}

// announceBackgroundJobs sends notifications/message when a backgrounded job
// completes (release plan 0.0.7, "Long-running work without polling"): a host
// that surfaces it can wake the agent rather than have it spend turns on
// job_status.
func (s *mcpServer) announceBackgroundJobs(completed <-chan events.Event) {
	go func() {
		for event := range completed {
			if event.Type == "JOB_COMPLETED" && s.background.finish(event) {
				s.announceJob(event)
			}
		}
	}()
}

func (s *mcpServer) announceJob(event events.Event) {
	if s.notify == nil {
		return
	}
	id := event.Payload["id"]
	s.notify("notifications/message", map[string]interface{}{
		"level":  "info",
		"logger": "arno",
		"data":   fmt.Sprintf("%s %s finished: %s — job_output %s for the log", id, event.Payload["kind"], event.Payload["summary"], id),
	})
}

// rpcNotification is a JSON-RPC message with no id: the server telling the
// client something without being asked.
type rpcNotification struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// lockedWriter serialises responses and notifications on one stdout, since
// progress is reported from a goroutine while a call is still being handled.
type lockedWriter struct {
	mu sync.Mutex
	w  *bufio.Writer
}

func (l *lockedWriter) write(v interface{}) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := writeMessage(l.w, v); err != nil {
		return err
	}
	return l.w.Flush()
}

// progressInterval is how often a long call reports that it is still working.
var progressInterval = 5 * time.Second

// reportProgress sends notifications/progress while a call runs, when the
// host asked for them with a progress token (release plan 0.0.7, "Long-running
// work without polling"). A check or test run of several minutes then reads
// as work in progress, not a hung server, to a host that shows or acts on it.
// It returns the function that stops the reports; no report is sent after it
// returns, so none lands after the call's response.
func (s *mcpServer) reportProgress(token interface{}, tool string) func() {
	if token == nil || s.notify == nil {
		return func() {}
	}
	done := make(chan struct{})
	var finished sync.WaitGroup
	finished.Add(1)
	started := time.Now()
	go func() {
		defer finished.Done()
		ticker := time.NewTicker(progressInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				elapsed := time.Since(started).Round(time.Second)
				s.notify("notifications/progress", map[string]interface{}{
					"progressToken": token,
					"progress":      time.Since(started).Seconds(),
					"message":       fmt.Sprintf("%s still running · %s", tool, elapsed),
				})
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			close(done)
			finished.Wait()
		})
	}
}

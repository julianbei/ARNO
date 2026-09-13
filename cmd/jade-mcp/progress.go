package main

import (
	"bufio"
	"fmt"
	"sync"
	"time"
)

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

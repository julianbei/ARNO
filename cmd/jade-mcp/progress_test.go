package main

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/julianbei/jade/internal/events"
	"github.com/julianbei/jade/internal/protocol"
)

func TestALongCallReportsProgressWhenTheHostAsks(t *testing.T) {
	previous := progressInterval
	progressInterval = 50 * time.Millisecond
	defer func() { progressInterval = previous }()

	server, _ := newTestMCPServer(t)
	var mu sync.Mutex
	var sent []map[string]interface{}
	server.notify = func(method string, params interface{}) {
		mu.Lock()
		defer mu.Unlock()
		if method == "notifications/progress" {
			sent = append(sent, params.(map[string]interface{}))
		}
	}
	if _, err := server.api.DeclareCommand(protocol.DeclareCommandRequest{Name: "slow", Run: "sleep 0.4"}); err != nil {
		t.Fatal(err)
	}

	call := func(meta string) {
		raw := json.RawMessage(`{"name":"jade.run_command","arguments":{"name":"slow"}` + meta + `}`)
		if _, err := server.handleToolCall(raw); err != nil {
			t.Fatal(err)
		}
	}

	call(`,"_meta":{"progressToken":"tok-1"}`)
	mu.Lock()
	count := len(sent)
	mu.Unlock()
	if count == 0 {
		t.Fatal("a call with a progress token should report progress while it runs")
	}
	if sent[0]["progressToken"] != "tok-1" {
		t.Fatalf("progress should carry the host's token, got %+v", sent[0])
	}
	time.Sleep(150 * time.Millisecond)
	mu.Lock()
	after := len(sent)
	mu.Unlock()
	if after != count {
		t.Fatalf("no progress may be sent after the call returned: %d then %d", count, after)
	}

	call("")
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != count {
		t.Fatalf("a call without a progress token must not report progress, got %d more", len(sent)-count)
	}
}
func TestABackgroundedJobIsAnnouncedWhenItCompletes(t *testing.T) {
	server, _ := newTestMCPServer(t)
	var mu sync.Mutex
	var messages []string
	server.notify = func(method string, params interface{}) {
		if method != "notifications/message" {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		messages = append(messages, params.(map[string]interface{})["data"].(string))
	}
	completed := make(chan events.Event, 8)
	server.announceBackgroundJobs(completed)
	count := func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(messages)
	}
	waitFor := func(want int) {
		deadline := time.Now().Add(2 * time.Second)
		for count() < want && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
	}
	done := func(id string) events.Event {
		return events.Event{Type: "JOB_COMPLETED", Payload: map[string]string{"id": id, "kind": "command:build", "summary": "built"}}
	}
	backgrounded := func(id string) {
		server.rememberBackgroundJobs(map[string]interface{}{"wait": false}, mcpToolResult{Content: []mcpTextContent{{Type: "text", Text: "running build · " + id}}})
	}

	backgrounded("job-7")
	completed <- done("job-7")
	waitFor(1)
	if count() != 1 || !strings.Contains(messages[0], "job-7 command:build finished: built") {
		t.Fatalf("a backgrounded job should be announced, got %v", messages)
	}

	completed <- done("job-8")
	time.Sleep(100 * time.Millisecond)
	if count() != 1 {
		t.Fatalf("a job no call backgrounded must not be announced, got %v", messages)
	}

	// Finished before its call recorded it: announced when it is recorded.
	completed <- done("job-9")
	time.Sleep(100 * time.Millisecond)
	backgrounded("job-9")
	waitFor(2)
	if count() != 2 || !strings.Contains(messages[1], "job-9") {
		t.Fatalf("a job that finished before its call returned should still be announced, got %v", messages)
	}

	server.rememberBackgroundJobs(map[string]interface{}{"wait": true}, mcpToolResult{Content: []mcpTextContent{{Type: "text", Text: "pass tests · job-10"}}})
	completed <- done("job-10")
	time.Sleep(100 * time.Millisecond)
	if count() != 2 {
		t.Fatalf("a waited job's verdict is in its response and must not be announced, got %v", messages)
	}
}

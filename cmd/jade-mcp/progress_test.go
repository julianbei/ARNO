package main

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

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

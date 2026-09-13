package main

import (
	"strings"
	"testing"

	"github.com/julianbei/jade/internal/protocol"
	"github.com/julianbei/jade/internal/telemetry"
)

func firstLine(out string) string {
	return strings.SplitN(out, "\n", 2)[0]
}

// A waited run that ran out of time used to read "running", the same as a
// call that never waited. It is its own outcome now, and never a pass.
func TestRunCommandThatOutlivesTheWaitSaysTimedOut(t *testing.T) {
	server, _ := newTestMCPServer(t)
	if _, err := server.api.DeclareCommand(protocol.DeclareCommandRequest{Name: "slow", Run: "sleep 5"}); err != nil {
		t.Fatalf("declare: %v", err)
	}

	out, err := callText(t, server, "jade.run_command", map[string]interface{}{"name": "slow", "timeoutSeconds": 1})
	if err != nil {
		t.Fatalf("run_command: %v", err)
	}
	if head := firstLine(out); !strings.HasPrefix(head, "timed out slow") {
		t.Fatalf("expected a timed out verdict, got:\n%s", out)
	}
}

// A tool that is not installed checked nothing. Saying FAIL sends the caller
// to fix code; saying pass is worse.
func TestRunCommandWithAMissingToolIsUnavailable(t *testing.T) {
	server, _ := newTestMCPServer(t)
	if _, err := server.api.DeclareCommand(protocol.DeclareCommandRequest{Name: "lint", Run: "jade-no-such-tool-4f2a --check"}); err != nil {
		t.Fatalf("declare: %v", err)
	}

	out, err := callText(t, server, "jade.run_command", map[string]interface{}{"name": "lint"})
	if err != nil {
		t.Fatalf("run_command: %v", err)
	}
	if head := firstLine(out); !strings.HasPrefix(head, "unavailable lint") {
		t.Fatalf("expected an unavailable verdict, got:\n%s", out)
	}
}

func TestCheckWithNoCommandLeadsWithUnavailable(t *testing.T) {
	server, root := newTestMCPServer(t)
	writeWorkspaceFile(t, root, "notes.md", "# nothing to build\n")

	out, err := callText(t, server, "jade.check", map[string]interface{}{"kind": "build"})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if head := firstLine(out); !strings.HasPrefix(head, "unavailable build") {
		t.Fatalf("expected the verdict word unavailable first, got:\n%s", out)
	}
}

// Telemetry uses the same set: a timed-out wait and a missing tool are the
// outcomes that send a caller to the shell, and both were uncounted before.
func TestValidationOutcomesReachTelemetry(t *testing.T) {
	cases := map[protocol.ValidationOutcome]telemetry.Outcome{
		protocol.OutcomeTimedOut:    telemetry.Timeout,
		protocol.OutcomeUnavailable: telemetry.Unavailable,
		protocol.OutcomeFailed:      telemetry.OK,
		protocol.OutcomePassed:      telemetry.OK,
		protocol.OutcomeRunning:     telemetry.OK,
	}
	for outcome, want := range cases {
		for _, response := range []interface{}{
			protocol.CheckResponse{Outcome: outcome},
			protocol.RunTestsResponse{Outcome: outcome},
			protocol.RunCommandResponse{Outcome: outcome},
			protocol.ApplyResponse{CheckOutcome: outcome},
		} {
			got, found := telemetry.ClassifyResponse(response)
			if !found || got != want {
				t.Errorf("%T with %q: got %q (found %v), want %q", response, outcome, got, found, want)
			}
		}
	}
}

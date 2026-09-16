package main

import (
	"strings"
	"testing"
	"time"

	"github.com/julianbei/arno/internal/protocol"
	"github.com/julianbei/arno/internal/telemetry"
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

	out, err := callText(t, server, "arno.run_command", map[string]interface{}{"name": "slow", "timeoutSeconds": 1})
	if err != nil {
		t.Fatalf("run_command: %v", err)
	}
	if head := firstLine(out); !strings.HasPrefix(head, "timed out slow") {
		t.Fatalf("expected a timed out verdict, got:\n%s", out)
	}
}

// A run that outlived its wait is joined, not restarted, when the same call
// comes again, and its summary names no tool the host may not have loaded.
func TestARepeatedCallJoinsTheRunThatOutlivedItsWait(t *testing.T) {
	server, _ := newTestMCPServer(t)
	if _, err := server.api.DeclareCommand(protocol.DeclareCommandRequest{Name: "slow", Run: "sleep 4"}); err != nil {
		t.Fatalf("declare: %v", err)
	}

	first, err := server.api.RunCommand(protocol.RunCommandRequest{Name: "slow", Wait: true, TimeoutSeconds: 1})
	if err != nil {
		t.Fatalf("run_command: %v", err)
	}
	if first.Outcome != protocol.OutcomeTimedOut {
		t.Fatalf("expected a timeout, got %+v", first)
	}
	if strings.Contains(first.Summary, "job_status") || !strings.Contains(first.Summary, "call run_command again") {
		t.Fatalf("the hint must point at the tool just called, got %q", first.Summary)
	}

	second, err := server.api.RunCommand(protocol.RunCommandRequest{Name: "slow", Wait: true, TimeoutSeconds: 10})
	if err != nil {
		t.Fatalf("run_command: %v", err)
	}
	if second.JobID != first.JobID {
		t.Fatalf("the second call started %s instead of joining %s", second.JobID, first.JobID)
	}
	if second.Outcome != protocol.OutcomePassed {
		t.Fatalf("the joined run should finish and pass, got %+v", second)
	}

	third, err := server.api.RunCommand(protocol.RunCommandRequest{Name: "slow", Wait: false})
	if err != nil {
		t.Fatalf("run_command: %v", err)
	}
	if third.JobID == first.JobID {
		t.Fatal("a finished run must not be joined")
	}
}

// A tool that is not installed checked nothing. Saying FAIL sends the caller
// to fix code; saying pass is worse.
func TestRunCommandWithAMissingToolIsUnavailable(t *testing.T) {
	server, _ := newTestMCPServer(t)
	if _, err := server.api.DeclareCommand(protocol.DeclareCommandRequest{Name: "lint", Run: "arno-no-such-tool-4f2a --check"}); err != nil {
		t.Fatalf("declare: %v", err)
	}

	out, err := callText(t, server, "arno.run_command", map[string]interface{}{"name": "lint"})
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

	out, err := callText(t, server, "arno.check", map[string]interface{}{"kind": "build"})
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

// The caller's timeout bounds how long it waits, not how long the command may
// run. Killing the process at the wait timeout ended every backgrounded run.
func TestABackgroundedCommandOutlivesItsWait(t *testing.T) {
	server, _ := newTestMCPServer(t)
	if _, err := server.api.DeclareCommand(protocol.DeclareCommandRequest{Name: "build-image", Run: "sleep 2; echo built"}); err != nil {
		t.Fatalf("declare: %v", err)
	}

	started, err := server.api.RunCommand(protocol.RunCommandRequest{Name: "build-image", Wait: false, TimeoutSeconds: 1})
	if err != nil {
		t.Fatalf("run_command: %v", err)
	}
	output, finished := server.api.WaitForJob(started.JobID, 10*time.Second)
	if !finished {
		t.Fatal("the command should finish on its own")
	}
	if output.Failed || !strings.Contains(output.Raw, "built") {
		t.Fatalf("a backgrounded command must not be killed at its wait timeout, got %+v", output)
	}
}

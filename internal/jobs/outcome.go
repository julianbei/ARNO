package jobs

import (
	"strings"

	"github.com/julianbei/arno/internal/protocol"
)

// killedMarker is what RunCommandWithTimeout appends to a command it killed.
const killedMarker = " and was killed"

// Outcome classifies a job the caller waited on; finished is Wait's second
// result.
//
// The order matters. A command killed for its timeout also exited non-zero,
// and a missing binary also "failed" — reporting either as failed would send
// the caller to fix code that was never checked. Exit status stays
// authoritative for everything else: a non-zero exit fails however reassuring
// the output reads, and failure wording fails a tool that exits 0 anyway.
func Outcome(output JobOutput, finished bool) protocol.ValidationOutcome {
	if !finished {
		return protocol.OutcomeTimedOut
	}
	text := output.Summary + "\n" + output.Raw
	switch {
	case strings.Contains(text, killedMarker):
		return protocol.OutcomeTimedOut
	case notInstalled(text):
		return protocol.OutcomeUnavailable
	case output.Failed || hasFailureMarkers(text):
		return protocol.OutcomeFailed
	default:
		return protocol.OutcomePassed
	}
}

// notInstalled recognises a command that could not run at all. Exit status
// 127 is the shell's "command not found"; the exec error is what a direct
// start reports; the last is discovery finding nothing to run.
func notInstalled(text string) bool {
	for _, marker := range []string{
		"exit status 127",
		"executable file not found",
		"no validation command configured",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// hasFailureMarkers looks for lines that only a failing run prints. Bare
// "error" or "fail" anywhere is not one: passing test suites log expected
// errors, and in a benchmark run cobra's green suite (it prints "Error: if any
// flags in the group ...") was reported as FAIL, so the agent re-ran every test
// to find out whether its edit was fine.
func hasFailureMarkers(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		for _, prefix := range []string{"--- FAIL", "FAIL\t", "FAIL ", "FAIL:", "FAILED", "panic:"} {
			if strings.HasPrefix(line, prefix) {
				return true
			}
		}
		if strings.Contains(line, "undefined:") || strings.Contains(line, "cannot find package") {
			return true
		}
	}
	return false
}

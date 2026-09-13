package jobs

import "github.com/julianbei/jade/internal/textutil"

// maxRawOutputBytes bounds what a job stores and returns as raw output.
//
// The threshold comes from a measurement of real agent tool traffic: only
// 2.8% of results exceeded 8,000 characters, but those results accounted for
// 31% of all tool output. The same shape applies here — almost every job is
// small, and the rare huge one
// (a failing `go test ./...` on a large repo) is what would flood an agent's
// context through job_output alone.
const maxRawOutputBytes = 8000

// clampRawOutput bounds a job's stored output. The shaping itself lives in
// textutil so that job output and diffs (8.2) cannot drift apart in how they
// truncate.
func clampRawOutput(output string) (string, int) {
	return textutil.Clamp(output, maxRawOutputBytes, "re-run with a narrower scope to see the middle")
}

// maxStoredOutputBytes bounds the output kept for paging through job_output.
// A failing test run's decisive middle is what the 8,000-byte clamp drops;
// keeping it lets a caller page to it instead of re-running the job.
const maxStoredOutputBytes = 1 << 20

// storedRawOutput is the output job_output pages through.
func storedRawOutput(output string) string {
	stored, _ := textutil.Clamp(output, maxStoredOutputBytes, "re-run with a narrower scope to see the middle")
	return stored
}

package protocol

// ValidationOutcome is how a check, test run or declared command ended.
//
// The set is closed. Before it, outcomes were free strings beside a Passed
// bool, and a waited check that ran out of time read "running" — the same as
// a call that never asked to wait. A caller, the renderer and telemetry can
// now switch on the outcome exhaustively, and only OutcomePassed is a pass:
// nothing unavailable, unfinished or killed can render or count as one.
type ValidationOutcome string

const (
	// OutcomePassed: the command finished, exited zero and reported no
	// failure.
	OutcomePassed ValidationOutcome = "passed"
	// OutcomeFailed: the command finished and reported broken code — a
	// non-zero exit or failure output. A verdict, not a Jade failure.
	OutcomeFailed ValidationOutcome = "failed"
	// OutcomeUnavailable: nothing was validated. No command was discovered
	// for this workspace, or the tool the command runs is not installed.
	OutcomeUnavailable ValidationOutcome = "unavailable"
	// OutcomeRunning: the caller chose not to wait. Poll the job.
	OutcomeRunning ValidationOutcome = "running"
	// OutcomeTimedOut: the caller waited and the command did not finish in
	// time — it may still be running, poll the job — or it was killed for
	// exceeding its own timeout.
	OutcomeTimedOut ValidationOutcome = "timed out"
)

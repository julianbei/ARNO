// Package telemetry records how arno's tools are actually used.
//
// Why this exists. arno's design bet is that giving a coding agent a wide
// enough tool surface keeps it inside arno, where edits are revision-tracked,
// validated and guarded — and that every gap sends the agent to bash, where
// none of that applies. That bet has been evaluated so far by the agent
// hand-writing docs/feedback.md from recollection, which is exactly the unreliable
// instrument the tooling was supposed to replace. This measures it instead.
//
// The question it is built to answer is narrower than "usage stats": which
// tool errors plausibly send a caller back to the shell. An ambiguous anchor,
// a symbol that was not found, a stale revision, a timeout — each is a moment
// where the arno path failed and bash was one keystroke away. Those are
// classified and counted separately from ordinary usage for that reason.
//
// # What is deliberately not recorded
//
// No tool arguments, no response bodies, and no error message text. Only the
// tool name, wall time, response size, and a failure *class*.
//
// Arguments carry source code, file paths and search queries; an error message
// routinely quotes the source line it failed on. A telemetry file containing
// those is a copy of the repository by another name — it could not be attached
// to a bug report, shared with the arno authors, or committed, which would
// defeat the entire purpose of collecting it. Keeping it content-free is what
// makes it shareable, and shareable is the point.
package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/julianbei/arno/internal/compat"
	"github.com/julianbei/arno/internal/protocol"
)

// Dir and File name the log's location. It shares .arno/ with the command
// registry, so a repo has exactly one arno-owned directory.
const (
	Dir  = ".arno"
	File = "telemetry.jsonl"
)

// RelPath is the log's path relative to the workspace root.
var RelPath = filepath.Join(Dir, File)

// maxLogBytes bounds the log. At roughly 120 bytes per record this is on the
// order of 40,000 calls, which is far more history than any analysis needs and
// small enough to read fully on every summary. On overflow the file is
// truncated rather than rotated into a second file: partial history is an
// acceptable loss for something that exists to show trends, and a rotation
// scheme is more machinery than the value justifies.
const maxLogBytes = 5 << 20

// Outcome classifies how a tool call ended.
type Outcome string

const (
	// OK is a call that returned without error.
	OK Outcome = "ok"

	// The failure classes below are the ones that plausibly end with the
	// caller running a shell command instead. They are the measurement this
	// package exists for.

	// NotFound: a symbol, file or anchor the caller named does not exist.
	NotFound Outcome = "not_found"
	// Ambiguous: an anchor or name matched more than once, so arno refused
	// rather than guessing.
	Ambiguous Outcome = "ambiguous"
	// StaleRevision: the workspace moved under a preconditioned edit.
	StaleRevision Outcome = "stale_revision"
	// Timeout: a job or command exceeded its bound.
	Timeout Outcome = "timeout"
	// InvalidInput: the request was malformed or missing a required field.
	InvalidInput Outcome = "invalid_input"
	// Unavailable: an external dependency (gopls, a formatter, git) is
	// missing or failed. Distinct from the rest because the fix is
	// installation, not usage.
	Unavailable Outcome = "unavailable"
	// Other: an error that did not match any known class. A rising count here
	// means the classifier needs a new case, so it is worth watching.
	Other Outcome = "other"
)

// Record is one tool call. Field names are short because this is written once
// per call and read in bulk.
type Record struct {
	Time time.Time `json:"t"`
	Tool string    `json:"tool"`
	// Target is a short hash of what the call was about (see TargetOf), so
	// sequences on one target can be found without storing paths or names.
	Target  string  `json:"target,omitempty"`
	Millis  int64   `json:"ms"`
	Bytes   int     `json:"bytes"`
	Outcome Outcome `json:"outcome"`
}

// Recorder appends tool-call records to the workspace log.
//
// Every method is safe to call concurrently and none of them can fail a tool
// call. A telemetry write that errors is dropped silently: losing a
// measurement is a small cost, while failing a working edit because the
// measurement could not be written would be an absurd trade — and would make
// arno less reliable than the bash it is competing with.
type Recorder struct {
	mu       sync.Mutex
	root     string
	disabled bool

	// path is the log's absolute location. inWorkspace says whether that is
	// inside root, which decides both how it is displayed and whether git
	// needs to be told to ignore it.
	path        string
	inWorkspace bool
}

// StateDirEnv names the environment variable that moves Arno's telemetry out
// of the workspace.
const StateDirEnv = "ARNO_STATE_DIR"

// New builds a Recorder for the workspace at root.
//
// Setting ARNO_TELEMETRY=0 disables recording entirely. An off switch is not
// optional for something that writes a file into the user's repository on
// every call.
//
// Setting ARNO_STATE_DIR puts the log under that directory instead of the
// workspace, in a subdirectory per workspace so several can share one state
// directory without mixing their measurements. This is the setting for a
// harness that roots Arno at a worktree it later commits wholesale.
func New(root string) *Recorder {
	recorder := &Recorder{
		root:     root,
		disabled: strings.TrimSpace(compat.Getenv("ARNO_TELEMETRY")) == "0",
	}
	if stateDir := strings.TrimSpace(compat.Getenv(StateDirEnv)); stateDir != "" {
		recorder.path = filepath.Join(stateDir, workspaceKey(root), File)
	} else {
		// compat.StatePath keeps writing to a .jade/ log that already exists,
		// so a session's history is not split in two by the rename.
		recorder.path = compat.StatePath(root, File)
		recorder.inWorkspace = true
	}
	return recorder
}

// DisplayPath is where the log lives, as a caller should be told it: relative
// when it is inside the workspace, absolute when it is not.
func (r *Recorder) DisplayPath() string {
	if r.inWorkspace {
		return RelPath
	}
	return r.path
}

// workspaceKey names a workspace's subdirectory under ARNO_STATE_DIR. The base
// name keeps it readable; the hash keeps two checkouts both called "app" apart.
func workspaceKey(root string) string {
	absolute, err := filepath.Abs(root)
	if err != nil {
		absolute = root
	}
	sum := sha256.Sum256([]byte(filepath.Clean(absolute)))
	base := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == ' ' {
			return '-'
		}
		return r
	}, filepath.Base(absolute))
	return base + "-" + hex.EncodeToString(sum[:])[:12]
}

// Record appends one call. err may be nil.
func (r *Recorder) Record(tool string, duration time.Duration, bytes int, err error) {
	r.RecordOutcome(tool, duration, bytes, Classify(err))
}

// RecordOutcome appends one call with an already-decided outcome, for callers
// that know more than the error does — notably a response that reports its own
// failure in band. See ClassifyResponse.
func (r *Recorder) RecordOutcome(tool string, duration time.Duration, bytes int, outcome Outcome) {
	r.RecordCall(tool, "", duration, bytes, outcome)
}

// RecordCall appends one call with its hashed target, which the confusion
// report uses to find a different tool asked about the same thing.
func (r *Recorder) RecordCall(tool string, target string, duration time.Duration, bytes int, outcome Outcome) {
	if r == nil || r.disabled || strings.TrimSpace(tool) == "" {
		return
	}
	if outcome == "" {
		outcome = OK
	}

	record := Record{
		Time:    time.Now().UTC(),
		Tool:    tool,
		Target:  target,
		Millis:  duration.Milliseconds(),
		Bytes:   bytes,
		Outcome: outcome,
	}

	line, marshalErr := json.Marshal(record)
	if marshalErr != nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.appendLocked(append(line, '\n'))
}

// RecordRejected records a call Arno refused before running anything — an
// unknown tool name — but only into a log that already exists.
//
// An unknown name is still a gap signal worth keeping: the agent expected a
// capability Arno lacks. But creating state in a workspace for a call that did
// nothing is exactly the stray file a harness then commits as part of someone
// else's change, so a rejected call never creates the log itself.
func (r *Recorder) RecordRejected(tool string, outcome Outcome) {
	if r == nil || r.disabled || strings.TrimSpace(tool) == "" {
		return
	}
	if _, err := os.Stat(filepath.Dir(r.path)); err != nil {
		return
	}
	r.RecordOutcome(tool, 0, 0, outcome)
}

func (r *Recorder) appendLocked(line []byte) {
	path := r.path
	if r.inWorkspace {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			// Before the first write, not after: once the file exists a
			// harness may already have seen it as untracked.
			excludeFromGit(r.root)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}

	if info, err := os.Stat(path); err == nil && info.Size() > maxLogBytes {
		_ = os.Truncate(path, 0)
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.Write(line)
}

// Read returns every record in the log, oldest first. A malformed line is
// skipped rather than failing the read: a partially written record from a
// killed process should not make the whole history unreadable.
func (r *Recorder) Read() ([]Record, error) {
	data, err := os.ReadFile(r.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	records := make([]Record, 0, 256)
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var record Record
		if json.Unmarshal([]byte(line), &record) != nil {
			continue
		}
		records = append(records, record)
	}
	return records, nil
}

// ToolStats aggregates one tool's calls.
type ToolStats struct {
	Tool     string
	Calls    int
	Errors   int
	Bytes    int64
	TotalMS  int64
	MaxMS    int64
	Failures []FailureCount
}

// FailureCount is one failure class and how often it occurred.
type FailureCount struct {
	Outcome Outcome
	Count   int
}

// Summary aggregates the log.
type Summary struct {
	Tools []ToolStats
	// Fallbacks counts failure classes across all tools — the headline
	// number, since each is a moment the arno path failed and the shell was
	// available.
	Fallbacks   []FailureCount
	TotalCalls  int
	TotalErrors int
	TotalBytes  int64
	Path        string
	Since       time.Time
}

// Summarize aggregates the whole log. Tools are ordered by call count
// descending, then by name, so the ranking leads and ties stay stable across
// calls rather than following map iteration order.
func (r *Recorder) Summarize() (Summary, error) {
	records, err := r.Read()
	if err != nil {
		return Summary{}, err
	}

	byTool := map[string]*ToolStats{}
	failuresByTool := map[string]map[Outcome]int{}
	overall := map[Outcome]int{}
	summary := Summary{Path: r.DisplayPath()}

	for _, record := range records {
		stats, ok := byTool[record.Tool]
		if !ok {
			stats = &ToolStats{Tool: record.Tool}
			byTool[record.Tool] = stats
			failuresByTool[record.Tool] = map[Outcome]int{}
		}

		stats.Calls++
		stats.Bytes += int64(record.Bytes)
		stats.TotalMS += record.Millis
		if record.Millis > stats.MaxMS {
			stats.MaxMS = record.Millis
		}
		if record.Outcome != OK {
			stats.Errors++
			failuresByTool[record.Tool][record.Outcome]++
			overall[record.Outcome]++
			summary.TotalErrors++
		}

		summary.TotalCalls++
		summary.TotalBytes += int64(record.Bytes)
		if summary.Since.IsZero() || record.Time.Before(summary.Since) {
			summary.Since = record.Time
		}
	}

	summary.Tools = make([]ToolStats, 0, len(byTool))
	for name, stats := range byTool {
		stats.Failures = sortedFailures(failuresByTool[name])
		summary.Tools = append(summary.Tools, *stats)
	}
	sort.Slice(summary.Tools, func(a, b int) bool {
		if summary.Tools[a].Calls != summary.Tools[b].Calls {
			return summary.Tools[a].Calls > summary.Tools[b].Calls
		}
		return summary.Tools[a].Tool < summary.Tools[b].Tool
	})
	summary.Fallbacks = sortedFailures(overall)
	return summary, nil
}

func sortedFailures(counts map[Outcome]int) []FailureCount {
	out := make([]FailureCount, 0, len(counts))
	for outcome, count := range counts {
		out = append(out, FailureCount{Outcome: outcome, Count: count})
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Count != out[b].Count {
			return out[a].Count > out[b].Count
		}
		return out[a].Outcome < out[b].Outcome
	})
	return out
}

// Reset clears the log. Exposed so a measurement run can start from a known
// state rather than requiring the caller to know where the file lives.
func (r *Recorder) Reset() error {
	err := os.Remove(r.path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// ClassifyResponse inspects a typed response for a failure reported *in band*
// — as a field on a successful response rather than as an error.
//
// Some arno tools report failure this way by design. read_symbol resolves a
// name to "exact", "ambiguous" or "not_found" and returns the candidates
// alongside, which is more useful than an error that throws them away. But the
// call still failed from the caller's point of view, and counting it as a
// success understates precisely the number this package exists to measure.
// Found when the first telemetry tests recorded a not_found read_symbol as OK.
//
// The bool reports whether an in-band outcome was found at all, so a caller
// can tell "this response says it failed" from "this response has nothing to
// say about it".
//
// # What is deliberately not treated as a failure
//
// A check, test run or declared command that reports Passed=false is a
// *verdict*, not a arno failure: the build is broken, and arno did its job by
// saying so clearly. Counting it here would swamp the fallback signal with
// ordinary red builds and make the number meaningless.
//
// Likewise a search or grep returning zero matches. "Nothing matches" is a
// correct and often expected answer; treating every empty result as a failure
// would punish the tools for being asked honest questions.
func ClassifyResponse(value interface{}) (Outcome, bool) {
	if response, ok := value.(protocol.InspectResponse); ok {
		switch response.Resolve.Status {
		case protocol.ResolutionNotFound:
			return NotFound, true
		case protocol.ResolutionAmbiguous:
			return Ambiguous, true
		case protocol.ResolutionExact:
			return OK, true
		}
	}
	switch response := value.(type) {
	case protocol.CheckResponse:
		return validationClass(response.Outcome)
	case protocol.RunTestsResponse:
		return validationClass(response.Outcome)
	case protocol.RunCommandResponse:
		return validationClass(response.Outcome)
	case protocol.ApplyResponse:
		return validationClass(response.CheckOutcome)
	}
	return OK, false
}

// validationClass maps a validation outcome onto a telemetry class, using the
// same closed set. A failed run stays OK for the reason above. A wait that ran
// out and a tool that is not installed are what send a caller to the shell,
// and both went uncounted while a timeout read "running".
func validationClass(outcome protocol.ValidationOutcome) (Outcome, bool) {
	switch outcome {
	case protocol.OutcomeTimedOut:
		return Timeout, true
	case protocol.OutcomeUnavailable:
		return Unavailable, true
	case protocol.OutcomePassed, protocol.OutcomeFailed, protocol.OutcomeRunning:
		return OK, true
	}
	return OK, false
}

// Classify maps an error to a failure class.
//
// It matches on message text because arno's errors cross package boundaries
// as wrapped strings rather than as a single sentinel hierarchy. That is
// fragile by nature, which is why Other exists as an explicit bucket: a rising
// Other count is the signal that this function has fallen behind the errors
// it is classifying, and is more useful than a misclassification that looks
// like a real trend.
func Classify(err error) Outcome {
	if err == nil {
		return OK
	}

	message := strings.ToLower(err.Error())
	switch {
	case contains(message, "ambiguous", "more than one match", "multiple matches"):
		return Ambiguous
	case contains(message, "stale revision", "revision mismatch", "expected revision"):
		return StaleRevision
	case contains(message, "timed out", "timeout", "deadline exceeded"):
		return Timeout
	// Unavailable is tested before NotFound on purpose: "executable file not
	// found" contains "not found", and classifying a missing gopls as a missing
	// symbol would point the reader at the wrong fix — install something, not
	// correct a name.
	case contains(message, "unavailable", "not installed", "executable file not found", "command not found"):
		return Unavailable
	case contains(message, "not found", "no such file", "does not exist", "no declarations", "no command", "unknown symbol"):
		return NotFound
	case contains(message, "is required", "invalid", "unknown op", "unknown check kind", "must be", "malformed"):
		return InvalidInput
	default:
		return Other
	}
}

func contains(message string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(message, needle) {
			return true
		}
	}
	return false
}

// ErrDisabled reports that telemetry is switched off, so a caller asking for a
// summary gets an explanation rather than a confusing empty one.
var ErrDisabled = errors.New("telemetry is disabled (ARNO_TELEMETRY=0)")

// SummaryLine renders the one-line headline used in responses.
func SummaryLine(s Summary) string {
	if s.TotalCalls == 0 {
		return "no tool calls recorded yet"
	}
	line := fmt.Sprintf("%d calls across %d tools, %d errors", s.TotalCalls, len(s.Tools), s.TotalErrors)
	if !s.Since.IsZero() {
		line += " since " + s.Since.Format("2006-01-02 15:04")
	}
	return line
}

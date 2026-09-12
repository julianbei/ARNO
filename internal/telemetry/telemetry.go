// Package telemetry records how jade's tools are actually used.
//
// Why this exists. jade's design bet is that giving a coding agent a wide
// enough tool surface keeps it inside jade, where edits are revision-tracked,
// validated and guarded — and that every gap sends the agent to bash, where
// none of that applies. That bet has been evaluated so far by the agent
// hand-writing feedback.md from recollection, which is exactly the unreliable
// instrument the tooling was supposed to replace. This measures it instead.
//
// The question it is built to answer is narrower than "usage stats": which
// tool errors plausibly send a caller back to the shell. An ambiguous anchor,
// a symbol that was not found, a stale revision, a timeout — each is a moment
// where the jade path failed and bash was one keystroke away. Those are
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
// to a bug report, shared with the jade authors, or committed, which would
// defeat the entire purpose of collecting it. Keeping it content-free is what
// makes it shareable, and shareable is the point.
package telemetry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/julianbei/jade/internal/protocol"
)

// Dir and File name the log's location. It shares .jade/ with the command
// registry, so a repo has exactly one jade-owned directory.
const (
	Dir  = ".jade"
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
	// Ambiguous: an anchor or name matched more than once, so jade refused
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
	Time    time.Time `json:"t"`
	Tool    string    `json:"tool"`
	Millis  int64     `json:"ms"`
	Bytes   int       `json:"bytes"`
	Outcome Outcome   `json:"outcome"`
}

// Recorder appends tool-call records to the workspace log.
//
// Every method is safe to call concurrently and none of them can fail a tool
// call. A telemetry write that errors is dropped silently: losing a
// measurement is a small cost, while failing a working edit because the
// measurement could not be written would be an absurd trade — and would make
// jade less reliable than the bash it is competing with.
type Recorder struct {
	mu       sync.Mutex
	root     string
	disabled bool
}

// New builds a Recorder for the workspace at root.
//
// Setting JADE_TELEMETRY=0 disables recording entirely. An off switch is not
// optional for something that writes a file into the user's repository on
// every call.
func New(root string) *Recorder {
	return &Recorder{
		root:     root,
		disabled: strings.TrimSpace(os.Getenv("JADE_TELEMETRY")) == "0",
	}
}

// Record appends one call. err may be nil.
func (r *Recorder) Record(tool string, duration time.Duration, bytes int, err error) {
	r.RecordOutcome(tool, duration, bytes, Classify(err))
}

// RecordOutcome appends one call with an already-decided outcome, for callers
// that know more than the error does — notably a response that reports its own
// failure in band. See ClassifyResponse.
func (r *Recorder) RecordOutcome(tool string, duration time.Duration, bytes int, outcome Outcome) {
	if r == nil || r.disabled || strings.TrimSpace(tool) == "" {
		return
	}
	if outcome == "" {
		outcome = OK
	}

	record := Record{
		Time:    time.Now().UTC(),
		Tool:    tool,
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

func (r *Recorder) appendLocked(line []byte) {
	if err := os.MkdirAll(filepath.Join(r.root, Dir), 0o755); err != nil {
		return
	}
	path := filepath.Join(r.root, RelPath)

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
	data, err := os.ReadFile(filepath.Join(r.root, RelPath))
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
	// number, since each is a moment the jade path failed and the shell was
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
	summary := Summary{Path: RelPath}

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
	err := os.Remove(filepath.Join(r.root, RelPath))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// ClassifyResponse inspects a typed response for a failure reported *in band*
// — as a field on a successful response rather than as an error.
//
// Some jade tools report failure this way by design. read_symbol resolves a
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
// *verdict*, not a jade failure: the build is broken, and jade did its job by
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
	return OK, false
}

// Classify maps an error to a failure class.
//
// It matches on message text because jade's errors cross package boundaries
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
var ErrDisabled = errors.New("telemetry is disabled (JADE_TELEMETRY=0)")

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

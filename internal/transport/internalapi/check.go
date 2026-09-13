package internalapi

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/julianbei/jade/internal/jobs"
	"github.com/julianbei/jade/internal/protocol"
)

// defaultCheckTimeout bounds a waited check. Longer than the runner's own
// 60s command timeout so a command that is about to be killed still reports
// its own outcome rather than this wait timing out first and reporting
// "still running" for something already dead.
const defaultCheckTimeout = 90 * time.Second

// maxCheckTimeout stops a caller from pinning the connection indefinitely.
const maxCheckTimeout = 300 * time.Second

// Check runs a validation command on demand.
//
// Every piece already existed: the kinds, the Makefile/npm/cargo discovery,
// the job runner. What was missing is that none of it could be *asked for* —
// validation only ever ran as a side effect of an edit. `go build ./...` and
// `go vet ./...` were the most frequent native shell commands across this
// whole project's dogfooding precisely because jade had no way to run them.
//
// Waits by default. Returning a job ID for a two-second vet, and making the
// caller poll for it, is the async-tax that kept the native command winning.
func (s *Server) Check(req protocol.CheckRequest) (protocol.CheckResponse, error) {
	kind, err := normalizeCheckKind(req.Kind)
	if err != nil {
		return protocol.CheckResponse{}, err
	}

	// Lint and codegen have no discovery: the repository's declared commands
	// of that kind are the answer.
	if kind == "lint" || kind == "codegen" {
		if response, ran := s.checkDeclared(req, kind); ran {
			return response, nil
		}
		if kind == "codegen" {
			return protocol.CheckResponse{
				Kind:    kind,
				Outcome: protocol.OutcomeUnavailable,
				Status:  "no command",
				Summary: "no codegen command declared — declare one with declare_command(name, run, kind: \"codegen\")",
			}, nil
		}
		// No declared lint: a typecheck is the closest check discovery has,
		// which is what lint meant before commands had kinds.
		kind = "typecheck"
	}

	dir := s.workspace.Root()
	target := strings.TrimSpace(req.Target)
	if target != "" {
		resolved, err := s.checkTarget(target)
		if err != nil {
			return protocol.CheckResponse{}, err
		}
		dir = resolved
	}

	command, found := jobs.DescribeValidationCommand(dir, kind)
	if !found {
		// Said up front rather than discovered from a failed run: the
		// ecosystem was not identified, so there is nothing to execute.
		summary := fmt.Sprintf("no %s command found: no Makefile target, package.json script, Cargo.toml, Maven/Gradle/sbt/Python/Ruby manifest or go.mod at the workspace root — declare one with declare_command and use run_command", kind)
		if target != "" {
			summary = fmt.Sprintf("no %s command found in %s: no Makefile target, package.json script or manifest there", kind, target)
		} else if projects := jobs.DiscoverProjects(dir); len(projects) > 0 {
			// A monorepo has no command at its root, and each project has
			// one. Name them rather than send the caller to the shell.
			names := make([]string, 0, len(projects))
			for _, project := range projects {
				names = append(names, project.Path+"/ ("+project.Manifest+")")
			}
			summary = fmt.Sprintf("no %s command at the workspace root; projects below it: %s — pass target to check one", kind, strings.Join(names, ", "))
		}
		return protocol.CheckResponse{
			Kind:    kind,
			Outcome: protocol.OutcomeUnavailable,
			Status:  "no command",
			Summary: summary,
		}, nil
	}
	if target != "" {
		command = "in " + target + ": " + command
	}
	if req.DryRun {
		return protocol.CheckResponse{Kind: kind, Status: "dry run", Command: command}, nil
	}

	jobID := s.jobs.Start(kind)
	s.jobs.RunValidationCommand(jobID, dir, kind)

	if !req.Wait {
		return protocol.CheckResponse{JobID: jobID, Kind: kind, Outcome: protocol.OutcomeRunning, Status: "running", Command: command}, nil
	}

	output, finished := s.jobs.Wait(jobID, checkTimeout(req.TimeoutSeconds))
	if !finished {
		return protocol.CheckResponse{
			JobID:   jobID,
			Kind:    kind,
			Outcome: protocol.OutcomeTimedOut,
			Status:  "running",
			Summary: fmt.Sprintf("%s did not finish within %s and is still running — poll job_status %s", kind, checkTimeout(req.TimeoutSeconds), jobID),
			Command: command,
		}, nil
	}

	outcome := finishedOutcome(output)
	passed := outcome == protocol.OutcomePassed
	s.workspace.RecordRun("check "+kind, string(outcome))
	return protocol.CheckResponse{
		JobID:   jobID,
		Kind:    kind,
		Outcome: outcome,
		Status:  output.Status,
		Passed:  passed,
		Summary: verdictSummary(passed, output.Summary, output.Raw),
		Command: command,
	}, nil
}

// checkTarget resolves check's target to a directory inside the workspace,
// refusing one that leaves it or does not exist.
func (s *Server) checkTarget(target string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(target))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("target %q is outside the workspace %s; name a directory inside it", target, s.workspace.Root())
	}
	dir := filepath.Join(s.workspace.Root(), clean)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "", fmt.Errorf("target %q is not a directory in the workspace", target)
	}
	return dir, nil
}

// normalizeCheckKind rejects unknown kinds rather than silently substituting
// one: running a different check than the caller asked for and reporting
// success would be worse than an error.
func normalizeCheckKind(kind string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", "build":
		return "build", nil
	case "typecheck", "vet":
		return "typecheck", nil
	case "lint":
		return "lint", nil
	case "codegen", "generate":
		return "codegen", nil
	case "tests", "test":
		return "tests", nil
	default:
		return "", fmt.Errorf("unknown check kind %q (want build, typecheck, tests, lint or codegen)", kind)
	}
}

func checkTimeout(seconds int) time.Duration {
	if seconds <= 0 {
		return defaultCheckTimeout
	}
	timeout := time.Duration(seconds) * time.Second
	if timeout > maxCheckTimeout {
		return maxCheckTimeout
	}
	return timeout
}

// verdictSummary produces the Summary line for a finished run.
//
// On failure it keeps the decisive-line extraction, which is good: the
// compiler diagnostic or FAIL line is exactly what the caller needs.
//
// On success it replaces it, because that extraction falls back to the *last*
// three lines when nothing looks like a failure — so an all-green `go test
// ./...` across 42 packages summarized to whichever three `ok` lines happened
// to sort last. That is correct and it reads as a partial run; confirming it
// was not cost a `cat Makefile` and a redundant `go test` during dogfooding.
// A count is both smaller and true.
func verdictSummary(passed bool, summary string, raw string) string {
	if !passed {
		return summary
	}
	return successSummary(raw)
}

// successSummary describes passing output without quoting an arbitrary slice
// of it.
//
// Short output is shown verbatim — `go build` and `gofmt -l` print nothing at
// all when clean, and a one-line success message is worth more than a count of
// it. Beyond that, recognised per-package result lines are counted, and
// anything else reports its size rather than a sample, since a sample of a log
// nobody asked for is the thing being removed.
func successSummary(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "no output"
	}

	lines := strings.Split(trimmed, "\n")
	passing, untested := 0, 0
	for _, line := range lines {
		field := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(field, "ok "), strings.HasPrefix(field, "ok\t"):
			passing++
		case strings.HasPrefix(field, "? "), strings.HasPrefix(field, "?\t"):
			untested++
		}
	}

	if passing > 0 {
		counted := fmt.Sprintf("%d packages ok", passing)
		if untested > 0 {
			counted += fmt.Sprintf(", %d with no test files", untested)
		}
		return counted
	}

	if len(lines) <= maxVerbatimSuccessLines {
		return trimmed
	}
	return fmt.Sprintf("%d lines of output", len(lines))
}

// maxVerbatimSuccessLines is how much passing output is worth quoting in full
// rather than counting.
const maxVerbatimSuccessLines = 3

// finishedOutcome classifies a job that finished while the caller waited.
// Every waited validation derives Passed from it, so a missing tool or a
// killed command can never be reported as passing.
func finishedOutcome(output jobs.JobOutput) protocol.ValidationOutcome {
	return jobs.Outcome(output, true)
}

// jobPassed decides pass/fail for a finished job.
//
// **Exit status is authoritative.** A non-zero exit is a failure however
// reassuring the output reads. This was the bug: the verdict used to come from
// the output text alone, so `echo "everything looks fine"; exit 3` reported
// `pass`. Tolerable for Go's build/vet/test, which announce their failures in
// words; wrong for the arbitrary project commands the repo command registry
// exists to run, since lint, codegen and migrate signal by exit code.
//
// The text scan is kept as a *secondary* signal, because some tools genuinely
// exit 0 while reporting a failure. So a job fails if it exited non-zero OR if
// its output contains failure markers — never only the latter.
func jobPassed(output jobs.JobOutput) bool {
	if output.Failed {
		return false
	}
	return !hasFailureMarkers(output.Summary, output.Raw)
}

// hasFailureMarkers looks for failure wording in a command's own output. Go's
// build, vet and test print nothing (or only "ok" lines) on success, so
// emptiness is the usual signal — but "FAIL" and "error" are checked
// explicitly so a tool that chatters on success cannot be read as passing when
// it did not.
func hasFailureMarkers(summary string, raw string) bool {
	combined := strings.ToLower(summary + "\n" + raw)
	for _, marker := range []string{"fail", "error", "cannot find", "undefined:", "panic:"} {
		if strings.Contains(combined, marker) {
			return true
		}
	}
	return false
}

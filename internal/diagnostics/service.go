package diagnostics

import (
	"bufio"
	"go/parser"
	"go/scanner"
	"go/token"
	"regexp"
	"strings"
	"sync"

	"github.com/julianbei/jade/internal/protocol"
)

// Service normalizes parser, LSP, and lint diagnostics.
type Service struct {
	root string

	languageServers LanguageServerCheck

	memoMu sync.Mutex
	memo   map[string]checkMemo
}

func NewService(root string) *Service {
	return &Service{root: root, memo: make(map[string]checkMemo)}
}

// Immediate runs the fast, synchronous checks that fit within an edit's
// latency budget and returns their diagnostics. Check has the same result
// with the name of the checker that produced it.
func (s *Service) Immediate(scope string) []protocol.Diagnostic {
	return s.Check(scope).Diagnostics
}

// checkGo is the Go path: a real syntax parse, then gopls for type-level
// diagnostics when the syntax is clean.
func (s *Service) checkGo(path string, absolute string) Result {
	fset := token.NewFileSet()
	_, err := parser.ParseFile(fset, absolute, nil, parser.AllErrors)
	if err == nil {
		// gopls not being installed, or timing out, is a different answer
		// from gopls running and finding nothing — so the checker named is
		// the one that actually ran.
		if diagnostics, ran := goplsCheck(s.root, path); ran {
			return Result{Diagnostics: diagnostics, Checker: "gopls"}
		}
		return Result{Checker: "go/parser (syntax only; gopls unavailable)"}
	}

	if errList, ok := err.(scanner.ErrorList); ok {
		diagnostics := make([]protocol.Diagnostic, 0, len(errList))
		for _, parseErr := range errList {
			diagnostics = append(diagnostics, protocol.Diagnostic{
				Level:   protocol.DiagnosticError,
				Path:    path,
				Line:    parseErr.Pos.Line,
				Column:  parseErr.Pos.Column,
				Message: parseErr.Msg,
			})
		}
		return Result{Diagnostics: diagnostics, Checker: "go/parser"}
	}

	return Result{
		Diagnostics: []protocol.Diagnostic{{Level: protocol.DiagnosticError, Path: path, Message: err.Error()}},
		Checker:     "go/parser",
	}
}

// scopePath extracts the file path portion of a scope, which may be a plain
// path (from replace_range) or a "path::Name@line" symbol ID (from
// replace_symbol).
func scopePath(scope string) string {
	if idx := strings.LastIndex(scope, "::"); idx >= 0 {
		return scope[:idx]
	}
	return scope
}

// DecisiveSummary extracts the smallest useful failure signal from raw output.
func DecisiveSummary(output string) string {
	lines := decisiveLines(output)
	if len(lines) == 0 {
		trimmed := strings.TrimSpace(output)
		if trimmed == "" {
			return "no output"
		}
		// Nothing looked like a failure, so fall back to the *end* of the
		// output rather than the start. Tools put their conclusion last
		// ("ok ... 0.2s", "Done in 1.4s", a summary count), while the start
		// of a long log is setup noise. Taking the first lines meant a
		// 4,000-line run summarized to whatever it happened to print first.
		return lastNLines(trimmed, 3)
	}

	return strings.Join(lines, " | ")
}

// decisiveMarker matches lines that plausibly signal a real failure across
// Go/TS/Rust tool output: FAIL/FAILED, panic:, a leading failure glyph, a
// bare ERROR, or a TypeScript/Rust-style coded error (TS1234, [E0308]).
var decisiveMarker = regexp.MustCompile(`(?i)(\bFAIL(ED)?\b|panic:|^[✕✗×]|\bERROR\b|error\s*(TS\d+|\[E\d+\]))`)

// compilerDiagnostic matches the file:line[:col]: form every mainstream
// compiler and linter emits — `main.go:42:3: undefined: X`,
// `src/app.ts:12:5: ...`, `src/lib.rs:7:1: ...`.
//
// Its absence was a real bug (found while building 8.1): a build log whose
// only error was `main.go:42:3: undefined: decisiveMiddleSymbol` matched no
// marker at all, so decisiveLines returned nothing and the summary fell back
// to the first three lines — pure filler. This is the single most common
// shape of failure output there is, and it was the one shape not recognized.
var compilerDiagnostic = regexp.MustCompile(`^\S+\.\w+:\d+(:\d+)?:\s+\S`)

// warningLine excludes lines that merely note a warning (e.g. go vet/lint
// noise) even if they happen to also contain a word like "error" elsewhere.
var warningLine = regexp.MustCompile(`(?i)^\s*warning`)

// isDecisive reports whether a line plausibly explains a failure. Warnings
// are excluded first: a warning that happens to contain the word "error" is
// still a warning, and letting it through would crowd out the real failure.
func isDecisive(line string) bool {
	if warningLine.MatchString(line) {
		return false
	}
	return decisiveMarker.MatchString(line) || compilerDiagnostic.MatchString(line)
}

// maxDecisiveLines bounds how many failure lines are surfaced ahead of raw
// output.
const maxDecisiveLines = 8

// decisiveLines extracts the smallest set of lines that actually explain a
// failure, consolidating what were three inconsistent heuristics into one
// canonical version:
//   - scans from the end of output backward, since a failing test/build
//     suite prints its errors last, not first;
//   - excludes warning-only lines so they don't crowd out real failures;
//   - deduplicates repeated lines (e.g. the same panic line echoed by
//     multiple goroutines);
//   - keeps at most maxDecisiveLines, restored to original (forward) order
//     for readability.
func decisiveLines(output string) []string {
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	lines := make([]string, 0, 64)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}

	if failures := testFailureLines(lines); len(failures) > 0 {
		return failures
	}

	seen := make(map[string]bool, maxDecisiveLines)
	reversed := make([]string, 0, maxDecisiveLines)
	for i := len(lines) - 1; i >= 0 && len(reversed) < maxDecisiveLines; i-- {
		line := lines[i]
		if !isDecisive(line) {
			continue
		}
		if seen[line] {
			continue
		}
		seen[line] = true
		reversed = append(reversed, withContinuation(lines, i))
	}

	out := make([]string, len(reversed))
	for i, line := range reversed {
		out[len(reversed)-1-i] = line
	}
	return out
}

// testFailureHeader matches the line a test runner prints to name a failing
// test: Go's `--- FAIL: TestX`, pytest's `FAILED tests/x.py::test_y`, cargo's
// `test name ... FAILED` and panics, ava's `✘`.
var testFailureHeader = regexp.MustCompile(`^(--- FAIL:|FAILED\s|test\s.+\s\.\.\.\sFAILED$|thread\s.+\spanicked at|✘|✖\s|●\s)`)

// packageResult matches a runner's per-package verdict, e.g.
// `FAIL	github.com/spf13/cobra	0.26s`.
var packageResult = regexp.MustCompile(`^FAIL\s+\S`)

// testFailureLines names the failing tests, each with the assertion lines
// that follow it, before anything else.
//
// Test output is full of the word "error" that is not a failure: tests that
// check error handling print their expected errors. A benchmark run on cobra
// summarised a failing `go test` as eight `Error: if any flags in the group…`
// lines from passing tests and dropped `--- FAIL: TestCompleteWithDisable…`,
// which came first. The agent chased the wrong test for eighteen calls. When a
// runner names its failures, those names and their assertions are the answer.
func testFailureLines(lines []string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(line string) {
		if !seen[line] && len(out) < maxDecisiveLines {
			seen[line] = true
			out = append(out, line)
		}
	}
	for i, line := range lines {
		if !testFailureHeader.MatchString(line) {
			continue
		}
		add(line)
		if detail := failureDetail(lines, i); detail != "" {
			add(detail)
		}
		// The lines that explain a named failure: its assertion
		// (file_test.go:76: ...) or its panic. Anything else — including an
		// `Error:` a later passing test prints — ends the block.
		for j := i + 1; j < len(lines) && j <= i+3 &&
			(compilerDiagnostic.MatchString(lines[j]) || strings.HasPrefix(lines[j], "panic:")); j++ {
			add(withContinuation(lines, j))
		}
	}
	if len(out) == 0 {
		return nil
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if packageResult.MatchString(lines[i]) {
			if !seen[lines[i]] {
				out = append(out, lines[i])
			}
			break
		}
	}
	return out
}

// maxContinuationLines bounds the block kept after a decisive line that
// introduces one; maxContinuationBytes bounds the joined result.
const (
	maxContinuationLines = 6
	maxContinuationBytes = 400
)

// withContinuation keeps the block a decisive line introduces. A Go test
// assertion reads `command_test.go:76: Expected to contain:` and puts the
// expected and actual values on the lines after it: the line alone says a
// test failed, not what it saw. A benchmark run spent a whole job_output call
// reading the part this dropped.
func withContinuation(lines []string, i int) string {
	line := lines[i]
	if !strings.HasSuffix(line, ":") {
		return line
	}
	parts := []string{line}
	for j := i + 1; j < len(lines) && j <= i+maxContinuationLines && !isDecisive(lines[j]); j++ {
		parts = append(parts, lines[j])
	}
	joined := strings.Join(parts, " ")
	if len(joined) > maxContinuationBytes {
		joined = joined[:maxContinuationBytes] + "…"
	}
	return joined
}

// avaFailureSuffixes are what ava appends to a failing test's title on its
// summary line and leaves off the title of the detail block.
var avaFailureSuffixes = []string{" Rejected promise returned by test", " Error thrown in test", " Timed out while running tests"}

var (
	// codeExcerptLine is a numbered source line in a runner's code frame:
	// ava's `4:   t.is(...)`, jest's `> 12 | expect(...)`.
	codeExcerptLine = regexp.MustCompile(`^(>\s*)?\d+\s*[:|]`)
	// bareLocation is a lone file:line a runner prints above its excerpt.
	bareLocation = regexp.MustCompile(`^[\w./@-]+:\d+(:\d+)?$`)
)

// maxDetailScan bounds how far a detail block is read.
const maxDetailScan = 30

// failureDetail returns what a JavaScript runner says about the failing test
// named on line i: the assertion's difference, or the error and its message,
// without the code frame and stack.
//
// ava names a failure as `✘ [fail]: title Rejected promise returned by test`
// on its summary line and explains it further down, under the bare title;
// jest explains under `● suite › title`. Summaries kept only the naming line,
// so in a benchmark run an agent re-ran one ky test seven times and wrote
// DEBUG tests to see what had failed.
func failureDetail(lines []string, i int) string {
	header := strings.TrimSpace(lines[i])
	titles := avaFailedTitles(lines)
	start := -1
	switch {
	case strings.HasPrefix(header, "●"):
		start = i + 1
	case strings.HasPrefix(header, "✘ [fail]: "):
		title := avaTitle(header)
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == title {
				start = j + 1
				break
			}
		}
	}
	if start < 0 {
		return ""
	}

	parts := []string{}
	for j := start; j < len(lines) && j < start+maxDetailScan; j++ {
		line := strings.TrimSpace(lines[j])
		if line == "─" || titles[line] || strings.HasPrefix(line, "●") || testFailureHeader.MatchString(line) {
			break
		}
		if line == "" || strings.HasPrefix(line, "at ") || strings.HasPrefix(line, "›") ||
			codeExcerptLine.MatchString(line) || bareLocation.MatchString(line) {
			continue
		}
		parts = append(parts, line)
	}
	joined := strings.Join(parts, " ")
	if len(joined) > maxContinuationBytes {
		joined = joined[:maxContinuationBytes] + "…"
	}
	return joined
}

// avaTitle is the test title on an ava summary line.
func avaTitle(header string) string {
	title := strings.TrimPrefix(strings.TrimSpace(header), "✘ [fail]: ")
	for _, suffix := range avaFailureSuffixes {
		title = strings.TrimSuffix(title, suffix)
	}
	return title
}

// avaFailedTitles is every failing title ava named, so one detail block
// stops where the next begins.
func avaFailedTitles(lines []string) map[string]bool {
	titles := map[string]bool{}
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "✘ [fail]: ") {
			titles[avaTitle(trimmed)] = true
		}
	}
	return titles
}

// lastNLines returns the final n non-empty lines, in original order. Used as
// the summary fallback: a tool's conclusion is at the end of its output, not
// the beginning.
func lastNLines(s string, n int) string {
	if n <= 0 {
		return ""
	}

	scanner := bufio.NewScanner(strings.NewReader(s))
	lines := make([]string, 0, 64)
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			lines = append(lines, line)
		}
	}

	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

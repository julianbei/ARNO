package diagnostics

import (
	"bufio"
	"go/parser"
	"go/scanner"
	"go/token"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/julianbei/jade/internal/protocol"
)

// Service normalizes parser, LSP, and lint diagnostics.
type Service struct {
	root string
}

func NewService(root string) *Service {
	return &Service{root: root}
}

// Immediate runs the fast, synchronous checks that fit within an edit's
// latency budget. Today that is a real Go syntax parse — a genuine
// "parser OK / parser ERROR at line N" signal, not a stub. Full type
// checking and linting are handled by the async job runner (jobs package)
// since they're too slow to run synchronously on every edit.
func (s *Service) Immediate(scope string) []protocol.Diagnostic {
	path := scopePath(scope)
	if path == "" || !strings.EqualFold(filepath.Ext(path), ".go") {
		return nil
	}

	absolute := path
	if !filepath.IsAbs(absolute) {
		absolute = filepath.Join(s.root, path)
	}

	fset := token.NewFileSet()
	_, err := parser.ParseFile(fset, absolute, nil, parser.AllErrors)
	if err == nil {
		// Syntax is clean; try gopls for deeper (type-level) diagnostics.
		// If gopls isn't available (or times out), fall through to nil —
		// this is a known limitation, not a claim that gopls checked and
		// found nothing (see goplsCheck's doc comment on that distinction).
		if diagnostics, ran := goplsCheck(s.root, path); ran {
			return diagnostics
		}
		return nil
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
		return diagnostics
	}

	return []protocol.Diagnostic{{
		Level:   protocol.DiagnosticError,
		Path:    path,
		Message: err.Error(),
	}}
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
	lines := make([]string, 0, 64)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lines = append(lines, line)
		}
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
		reversed = append(reversed, line)
	}

	out := make([]string, len(reversed))
	for i, line := range reversed {
		out[len(reversed)-1-i] = line
	}
	return out
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

package jobs

import (
	"regexp"
	"strconv"
	"strings"
)

// TestSummary is what a test run's output says about the tests themselves:
// how many passed, failed and were skipped, the first failure and why, and
// causes in the environment rather than the code.
//
// A passing run used to be reported as "103 lines of output" and a failing
// one as "--- FAIL: TestPlugin". In the 0.0.7 benchmark rerun agents re-ran
// suites only to learn whether their new test had run at all, or what the
// failing assertion was, and one stopped early because nothing said the
// remaining failures were missing Playwright browsers.
type TestSummary struct {
	// Found is false when the output is in no recognised test-runner format.
	Found   bool
	Passed  int
	Failed  int
	Skipped int
	// FirstFailure names the first failing test; Detail is its assertion or
	// panic line when the output carries one.
	FirstFailure string
	Detail       string
	// Environment lists causes the output names that fail tests regardless of
	// the change under test.
	Environment []string
}

var (
	cargoResult  = regexp.MustCompile(`test result: (?:ok|FAILED)\. (\d+) passed; (\d+) failed; (\d+) ignored`)
	cargoFailed  = regexp.MustCompile(`(?m)^test (\S+) \.\.\. FAILED`)
	pytestResult = regexp.MustCompile(`(?m)^=*\s*((?:\d+ (?:passed|failed|skipped|errors?|xfailed|xpassed|deselected|warnings?)(?:, )?)+) in [\d.]+s`)
	pytestFailed = regexp.MustCompile(`(?m)^(?:FAILED|ERROR) (\S+)(?: - (.*))?$`)
	jestResult   = regexp.MustCompile(`(?m)^\s*Tests:?\s+((?:\d+ \w+(?:, | \| )?)+)`)
	jestFailed   = regexp.MustCompile(`(?m)^\s*●\s+(.+)$`)
	vitestFailed = regexp.MustCompile(`(?m)^\s*FAIL\s+(.+)$`)
	avaCount     = regexp.MustCompile(`(?m)^\s*(\d+) (?:tests? (passed|failed|skipped|todo)|(known failures?))`)
	avaFailed    = regexp.MustCompile(`✘ \[fail\]: (.+)`)
	goResult     = regexp.MustCompile(`(?m)^--- (PASS|FAIL|SKIP): (\S+)`)
	goFailed     = regexp.MustCompile(`(?m)^--- FAIL: (\S+)`)
	countPiece   = regexp.MustCompile(`(\d+) (passed|failed|skipped|ignored|todo|errors?|xfailed|xpassed)`)
	detailLine   = regexp.MustCompile(`(?i)(assert|expected|panicked|mismatch|differ|error|\.go:\d+:)`)
	goDetailLine = regexp.MustCompile(`\.go:\d+:`)
)

// environmentMarkers are output lines that mean the machine lacks something
// the tests need. They are named, never used to turn a failure into a pass.
var environmentMarkers = []struct{ marker, cause string }{
	{"browserType.launch: Executable doesn't exist", "Playwright browsers are not installed"},
	{"Missing dependencies for SOCKS support", "SOCKS support (PySocks) is not installed"},
}

// SummarizeTests reads go test, cargo test, pytest, jest, vitest and ava
// output. Output in none of those formats returns Found false.
func SummarizeTests(raw string) TestSummary {
	var s TestSummary
	switch {
	case cargoResult.MatchString(raw):
		for _, m := range cargoResult.FindAllStringSubmatch(raw, -1) {
			s.Passed += atoi(m[1])
			s.Failed += atoi(m[2])
			s.Skipped += atoi(m[3])
		}
		if m := cargoFailed.FindStringSubmatch(raw); m != nil {
			s.FirstFailure = m[1]
			// The panic is printed in the failures section, far below the
			// line that names the test.
			if at := strings.Index(raw, "---- "+m[1]+" stdout ----"); at >= 0 {
				s.Detail = detailAfter(raw[at:], detailLine)
			}
		}
	case pytestResult.MatchString(raw):
		results := pytestResult.FindAllStringSubmatch(raw, -1)
		s.addPieces(results[len(results)-1][1])
		if m := pytestFailed.FindStringSubmatch(raw); m != nil {
			s.FirstFailure = m[1]
			s.Detail = clipDetail(m[2])
		}
	case jestResult.MatchString(raw):
		results := jestResult.FindAllStringSubmatch(raw, -1)
		s.addPieces(results[len(results)-1][1])
		if !s.firstFailure(raw, jestFailed) {
			s.firstFailure(raw, vitestFailed)
		}
	case avaCount.MatchString(raw):
		for _, m := range avaCount.FindAllStringSubmatch(raw, -1) {
			n := atoi(m[1])
			switch {
			case m[3] != "":
				s.Skipped += n
			case m[2] == "passed":
				s.Passed += n
			case m[2] == "failed":
				s.Failed += n
			default:
				s.Skipped += n
			}
		}
		s.firstFailure(raw, avaFailed)
	case goResult.MatchString(raw):
		for _, m := range goResult.FindAllStringSubmatch(raw, -1) {
			switch m[1] {
			case "PASS":
				s.Passed++
			case "FAIL":
				s.Failed++
			default:
				s.Skipped++
			}
		}
		if loc := goFailed.FindStringSubmatchIndex(raw); loc != nil {
			s.FirstFailure = raw[loc[2]:loc[3]]
			// Without -v the test's log follows its FAIL line; with -v it was
			// streamed before it.
			s.Detail = detailAfter(raw[loc[1]:], goDetailLine)
			if s.Detail == "" {
				s.Detail = detailBefore(raw[:loc[0]], goDetailLine)
			}
		}
	default:
		return s
	}
	s.Found = true
	for _, known := range environmentMarkers {
		if strings.Contains(raw, known.marker) {
			s.Environment = append(s.Environment, known.cause)
		}
	}
	return s
}

// Counts renders the counts, or says that no test ran.
func (s TestSummary) Counts() string {
	var parts []string
	if s.Failed > 0 {
		parts = append(parts, strconv.Itoa(s.Failed)+" failed")
	}
	if s.Passed > 0 {
		parts = append(parts, strconv.Itoa(s.Passed)+" passed")
	}
	if s.Skipped > 0 {
		parts = append(parts, strconv.Itoa(s.Skipped)+" skipped")
	}
	if len(parts) == 0 {
		return "0 tests ran"
	}
	return strings.Join(parts, ", ")
}

// FailureLine renders a failed run: the counts, the first failure and its
// detail, falling back to the decisive summary when no test is named, and the
// environment causes.
func (s TestSummary) FailureLine(fallback string) string {
	line := s.Counts()
	switch {
	case s.FirstFailure != "":
		line += " — first failure: " + clipDetail(s.FirstFailure)
		if s.Detail != "" {
			line += ": " + s.Detail
		}
	case fallback != "":
		line += " · " + fallback
	}
	if len(s.Environment) > 0 {
		line += " — the output also shows " + strings.Join(s.Environment, "; ") + ", which fails tests whatever the change"
	}
	return line
}

func (s *TestSummary) addPieces(text string) {
	for _, m := range countPiece.FindAllStringSubmatch(text, -1) {
		n := atoi(m[1])
		switch m[2] {
		case "passed", "xpassed":
			s.Passed += n
		case "failed", "error", "errors":
			s.Failed += n
		default:
			s.Skipped += n
		}
	}
}

// firstFailure names the first match of re and looks for its detail below it.
func (s *TestSummary) firstFailure(raw string, re *regexp.Regexp) bool {
	loc := re.FindStringSubmatchIndex(raw)
	if loc == nil {
		return false
	}
	s.FirstFailure = strings.TrimSpace(raw[loc[2]:loc[3]])
	s.Detail = detailAfter(raw[loc[1]:], detailLine)
	return true
}

// maxDetailScan bounds how far from a failure's name its detail is looked for.
const maxDetailScan = 20

// detailAfter is the first detail line in the lines that follow; a panic line
// takes the message on the line after it too.
func detailAfter(text string, re *regexp.Regexp) string {
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines) && i < maxDetailScan; i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || !re.MatchString(line) {
			continue
		}
		if strings.Contains(line, "panicked at") {
			for _, next := range lines[i+1:] {
				if next = strings.TrimSpace(next); next != "" {
					line += " " + next
					break
				}
			}
		}
		return clipDetail(line)
	}
	return ""
}

// detailBefore is the last detail line among the lines just before.
func detailBefore(text string, re *regexp.Regexp) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-maxDetailScan; i-- {
		if line := strings.TrimSpace(lines[i]); re.MatchString(line) {
			return clipDetail(line)
		}
	}
	return ""
}

// maxDetailBytes keeps one failure's detail to a line an agent can read.
const maxDetailBytes = 200

func clipDetail(text string) string {
	text = strings.TrimSpace(text)
	if len(text) <= maxDetailBytes {
		return text
	}
	return text[:maxDetailBytes] + "…"
}

func atoi(text string) int {
	n, _ := strconv.Atoi(text)
	return n
}

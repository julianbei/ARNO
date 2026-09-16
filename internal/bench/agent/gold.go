package agent

import (
	"bufio"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// Gold is the reference fix's footprint: the non-test files it changed and
// the pre-fix lines it removed or sat next to. The yield metrics ask how much
// of that footprint the agent's tool results delivered, and at what cost in
// tokens. SWE-Explore and Agent Retrieval Bench both find that line-level
// coverage under a context budget tracks repair success better than
// file-level recall does, so "relevant code delivered per token returned" is
// the number a result formatter is tuned against — not whether search found
// every possibly related file.
type Gold struct {
	Files []string `json:"files"`
	Lines []string `json:"lines"`
}

// Yield is how much of a task's gold set one run's tool results showed the
// agent, and what that cost. Nil on a run without a gold set.
type Yield struct {
	GoldFiles     int `json:"goldFiles"`
	GoldFilesSeen int `json:"goldFilesSeen"`
	GoldLines     int `json:"goldLines"`
	GoldLinesSeen int `json:"goldLinesSeen"`
	// ObservationTokens is every tool result's size in tokens (bytes / 4),
	// the same estimate the budgets use.
	ObservationTokens int `json:"observationTokens"`
	// FirstGoldCall is the index of the first call whose result showed a
	// gold line — how long localisation took — or 0 when none did.
	FirstGoldCall int `json:"firstGoldCall"`
}

// FileRecall is the share of gold files a tool result named.
func (y Yield) FileRecall() float64 {
	if y.GoldFiles == 0 {
		return 0
	}
	return float64(y.GoldFilesSeen) / float64(y.GoldFiles)
}

// LineRecall is the share of gold lines a tool result showed.
func (y Yield) LineRecall() float64 {
	if y.GoldLines == 0 {
		return 0
	}
	return float64(y.GoldLinesSeen) / float64(y.GoldLines)
}

// LinesPerKToken is gold lines shown per thousand tokens of tool result: the
// yield of the observations the agent paid for.
func (y Yield) LinesPerKToken() float64 {
	if y.ObservationTokens == 0 {
		return 0
	}
	return float64(y.GoldLinesSeen) / (float64(y.ObservationTokens) / 1000)
}

// minGoldLineLength drops lines too short to identify anything: a brace, a
// bare `return`, `}`, `end`.
const minGoldLineLength = 12

// goldFor derives a task's gold set from the source checkout: the diff from
// the task's starting commit to VerifyFrom, the fix. Hidden test paths and
// files that look like tests are left out — the agent is meant to write the
// fix, not to see the tests. A task without VerifyFrom has no gold set.
func goldFor(repo string, task Task, base string) (*Gold, error) {
	if task.VerifyFrom == "" || base == "" {
		return nil, nil
	}
	out, err := exec.Command("git", "-C", repo, "diff", "--name-only", base, task.VerifyFrom).Output()
	if err != nil {
		return nil, fmt.Errorf("gold files for %s: %v", task.ID, err)
	}
	hidden := map[string]bool{}
	for _, path := range task.VerifyPaths {
		hidden[path] = true
	}
	for path := range task.VerifyFiles {
		hidden[path] = true
	}
	var files []string
	for _, path := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if path == "" || hidden[path] || isTestPath(path) {
			continue
		}
		files = append(files, path)
	}
	if len(files) == 0 {
		return nil, nil
	}
	sort.Strings(files)

	args := append([]string{"-C", repo, "diff", "-U3", base, task.VerifyFrom, "--"}, files...)
	patch, err := exec.Command("git", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("gold lines for %s: %v", task.ID, err)
	}
	return &Gold{Files: files, Lines: goldLines(string(patch))}, nil
}

// goldLines is every distinct pre-fix line a patch removed or kept as hunk
// context: what an agent must have seen to make the same change. Added lines
// are the answer, not the footprint, and are left out.
func goldLines(patch string) []string {
	seen := map[string]bool{}
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(patch))
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	inHunk := false
	for scanner.Scan() {
		row := scanner.Text()
		switch {
		case strings.HasPrefix(row, "diff --git"):
			inHunk = false
			continue
		case strings.HasPrefix(row, "@@"):
			inHunk = true
			continue
		case !inHunk, row == "", strings.HasPrefix(row, "+"), strings.HasPrefix(row, "\\"):
			continue
		}
		line := strings.TrimSpace(row[1:])
		if len(line) < minGoldLineLength || seen[line] {
			continue
		}
		seen[line] = true
		lines = append(lines, line)
	}
	return lines
}

// isTestPath recognises test files by the conventions of the benchmark's
// languages, so a fix commit's own test changes never count as gold.
func isTestPath(path string) bool {
	lower := strings.ToLower(path)
	base := lower[strings.LastIndex(lower, "/")+1:]
	for _, dir := range []string{"test/", "tests/", "testdata/", "__tests__/"} {
		if strings.HasPrefix(lower, dir) || strings.Contains(lower, "/"+dir) {
			return true
		}
	}
	return strings.HasSuffix(base, "_test.go") || strings.HasSuffix(base, "_test.py") || strings.HasSuffix(base, "_test.rb") ||
		strings.HasPrefix(base, "test_") || strings.Contains(base, ".test.") || strings.Contains(base, ".spec.")
}

// mentionsPath reports whether text names path as a whole path segment
// sequence: args.go is not mentioned by flag_args.go, but src/args.go and
// /tmp/x/workspace/args.go both mention args.go.
func mentionsPath(text string, path string) bool {
	from := 0
	for {
		index := strings.Index(text[from:], path)
		if index < 0 {
			return false
		}
		at := from + index
		if at == 0 || !isPathByte(text[at-1]) {
			end := at + len(path)
			if end == len(text) || !isPathByte(text[end]) {
				return true
			}
		}
		from = at + 1
	}
}

func isPathByte(b byte) bool {
	return b == '_' || b == '-' || b == '.' || ('0' <= b && b <= '9') || ('a' <= b && b <= 'z') || ('A' <= b && b <= 'Z')
}

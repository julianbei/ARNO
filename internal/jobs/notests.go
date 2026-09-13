package jobs

import (
	"regexp"
	"strings"
)

var (
	// cargoRanTests matches a test binary that ran at least one test.
	cargoRanTests = regexp.MustCompile(`running [1-9]\d* tests?`)
	// goRanTests matches a package that ran tests: its plain ok line, which
	// go test ends with [no tests to run] when a -run pattern matched none.
	goRanTests = regexp.MustCompile(`(?m)^(=== RUN|--- PASS|ok\s+\S+\s+[\d.]+s\s*$)`)
)

// NoTestsRan reports test output in which the runner ran no test at all.
// cargo and go test exit 0 then, so a test name that matched nothing read as
// a pass.
func NoTestsRan(output string) bool {
	switch {
	case strings.Contains(output, "running 0 tests"):
		return !cargoRanTests.MatchString(output)
	case strings.Contains(output, "[no tests to run]"):
		return !goRanTests.MatchString(output)
	}
	return false
}

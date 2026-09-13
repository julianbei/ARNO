package diagnostics

import (
	"strings"
	"testing"
)

// cargoFailureOutput is cargo test's real output for one failed assert_eq!,
// one index panic and one pass.
const cargoFailureOutput = `     Running tests/regression.rs (target/debug/deps/regression-d1dcadddc5ed2014)

running 3 tests
test passes ... ok
test r2658_null_data_line_regexp ... FAILED
test r3180_look_around_panic ... FAILED

failures:

---- r2658_null_data_line_regexp stdout ----

thread 'r2658_null_data_line_regexp' (101856615) panicked at tests/regression.rs:3:5:
assertion ` + "`left == right`" + ` failed: null data must split lines
  left: "a\0b"
 right: "a\nb"

---- r3180_look_around_panic stdout ----

thread 'r3180_look_around_panic' (101856616) panicked at tests/regression.rs:9:14:
index out of bounds: the len is 0 but the index is 3
note: run with ` + "`RUST_BACKTRACE=1`" + ` environment variable to display a backtrace


failures:
    r2658_null_data_line_regexp
    r3180_look_around_panic

test result: FAILED. 1 passed; 2 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.00s

error: test failed, to rerun pass ` + "`--test regression`"

func TestCargoFailureSummaryKeepsThePanicMessage(t *testing.T) {
	got := strings.Join(decisiveLines(cargoFailureOutput), "\n")
	for _, want := range []string{
		"test r2658_null_data_line_regexp ... FAILED",
		"assertion `left == right` failed: null data must split lines left: \"a\\0b\" right: \"a\\nb\"",
		"index out of bounds: the len is 0 but the index is 3",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "RUST_BACKTRACE") {
		t.Errorf("the backtrace note must be dropped:\n%s", got)
	}
}

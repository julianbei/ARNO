package diagnostics

import (
	"strings"
	"testing"
)

// pytestFailureOutput is pytest 9's real output for one failed assertion, one
// raised exception and one pass.
const pytestFailureOutput = `============================= test session starts ==============================
collected 3 items

tests/test_probe.py FF.                                                  [100%]

=================================== FAILURES ===================================
_________________ test_parse_content_type_param_without_value __________________

    def test_parse_content_type_param_without_value():
>       assert parse_content_type("text/plain; charset") == ("text/plain", {"charset": True})
E       AssertionError: assert ('text/plain', {}) == ('text/plain'...arset': True})
E         
E         At index 1 diff: {} != {'charset': True}
E         Use -v to get more diff

tests/test_probe.py:5: AssertionError
_________________________________ test_raises __________________________________

    def test_raises():
>       raise KeyError("missing header")
E       KeyError: 'missing header'

tests/test_probe.py:8: KeyError
=========================== short test summary info ============================
FAILED tests/test_probe.py::test_parse_content_type_param_without_value - Ass...
FAILED tests/test_probe.py::test_raises - KeyError: 'missing header'
========================= 2 failed, 1 passed in 0.02s ==========================`

func TestPytestFailureSummaryKeepsTheAssertion(t *testing.T) {
	got := strings.Join(decisiveLines(pytestFailureOutput), "\n")
	for _, want := range []string{
		"FAILED tests/test_probe.py::test_parse_content_type_param_without_value - Ass...",
		"AssertionError: assert ('text/plain', {}) == ('text/plain'...arset': True}) At index 1 diff: {} != {'charset': True}",
		"KeyError: 'missing header'",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	for _, noise := range []string{"Use -v", "def test_raises", "raise KeyError(\"missing header\")\n"} {
		if strings.Contains(got, noise) {
			t.Errorf("source or hint %q kept in:\n%s", noise, got)
		}
	}
}

func TestPytestDetailForATestInAClass(t *testing.T) {
	lines := []string{
		"___ TestUtils.test_parse ___",
		"E       assert 1 == 2",
		"FAILED tests/test_utils.py::TestUtils::test_parse - assert 1 == 2",
	}
	if got := pytestDetail(lines, lines[2]); got != "assert 1 == 2" {
		t.Errorf("got %q", got)
	}
}

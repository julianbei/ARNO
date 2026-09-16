package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Gold lines are the old side of every hunk: removed lines and context, never
// added lines, headers, or lines too short to identify anything.
func TestGoldLinesTakesTheOldSideOfHunks(t *testing.T) {
	patch := strings.Join([]string{
		"diff --git a/args.go b/args.go",
		"--- a/args.go",
		"+++ b/args.go",
		"@@ -10,4 +10,4 @@ func parse(args []string) []string {",
		" \tif len(args) == 0 {",
		"-\t\treturn args[:0]",
		"+\t\treturn append([]string(nil), args[:0]...)",
		" \t}",
		" \treturn append([]string(nil), args...)",
		"diff --git a/b.go b/b.go",
		"--- a/b.go",
		"+++ b/b.go",
		"@@ -1,2 +1,2 @@",
		"-\t\treturn args[:0]",
		"+\t\treturn nil",
		"\\ No newline at end of file",
	}, "\n")
	got := goldLines(patch)
	want := []string{"if len(args) == 0 {", "return args[:0]", "return append([]string(nil), args...)"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("gold lines: got %q, want %q", got, want)
	}
}

func TestIsTestPath(t *testing.T) {
	for path, want := range map[string]bool{
		"command.go": false, "command_test.go": true, "test/helper.go": true, "src/tests/x.ts": true,
		"source/index.test.ts": true, "source/index.ts": false, "tests/test_api.py": true, "src/requests/models.py": false,
		"crates/core/src/lib.rs": false, "crates/core/testdata/x.txt": true, "attest.go": false,
	} {
		if got := isTestPath(path); got != want {
			t.Errorf("isTestPath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestMentionsPathMatchesWholeSegments(t *testing.T) {
	if !mentionsPath("read src/args.go:12", "args.go") || !mentionsPath("args.go", "args.go") || !mentionsPath("/tmp/x/workspace/args.go\n", "args.go") {
		t.Fatal("a path at a segment boundary was not recognised")
	}
	if mentionsPath("flag_args.go", "args.go") || mentionsPath("args.gox", "args.go") || mentionsPath("src/xargs.go", "args.go") {
		t.Fatal("a path inside a longer name was taken as a mention")
	}
}

// The gold set is the fix's diff from the task's starting commit, minus the
// hidden tests and anything that looks like one.
func TestGoldForReadsTheFixFromTheSourceCheckout(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	git := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@localhost"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(name string, content string) {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q")
	write("args.go", "package main\n\nfunc parse(args []string) []string {\n\tif len(args) == 0 {\n\t\treturn args[:0]\n\t}\n\treturn args\n}\n")
	write("args_test.go", "package main\n")
	write("README.md", "# demo\n")
	git("add", "-A")
	git("commit", "-q", "-m", "base")
	base := git("rev-parse", "HEAD")

	write("args.go", "package main\n\nfunc parse(args []string) []string {\n\tif len(args) == 0 {\n\t\treturn nil\n\t}\n\treturn append([]string(nil), args...)\n}\n")
	write("args_test.go", "package main\n\nfunc TestParse() {}\n")
	write("README.md", "# demo\n\nparse copies its input now, which is a long enough line.\n")
	git("add", "-A")
	git("commit", "-q", "-m", "fix")
	fix := git("rev-parse", "HEAD")

	task := Task{ID: "t", VerifyFrom: fix, VerifyPaths: []string{"args_test.go"}}
	gold, err := goldFor(repo, task, base)
	if err != nil {
		t.Fatal(err)
	}
	if gold == nil || strings.Join(gold.Files, ",") != "README.md,args.go" {
		t.Fatalf("expected the fix's non-test files, got %+v", gold)
	}
	joined := strings.Join(gold.Lines, "|")
	for _, want := range []string{"return args[:0]", "if len(args) == 0 {"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected gold line %q in %q", want, joined)
		}
	}
	if strings.Contains(joined, "func TestParse") || strings.Contains(joined, "append([]string(nil)") {
		t.Errorf("test or added lines leaked into the gold set: %q", joined)
	}

	if gold, err := goldFor(repo, Task{ID: "no-fix"}, base); err != nil || gold != nil {
		t.Fatalf("a task without verifyFrom has no gold set, got %+v, %v", gold, err)
	}
}

// Yield is measured from what the tool results showed: which gold files were
// named, which gold lines appeared, when the first did, and per how many
// tokens of result.
func TestAnalyzeMeasuresYieldAgainstTheGoldSet(t *testing.T) {
	lines := []string{
		`{"type":"assistant","request_id":"r1","message":{"content":[{"type":"tool_use","id":"a","name":"Grep","input":{"pattern":"parse"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"a","content":"src/other.go:3:func parse()"}]}}`,
		`{"type":"assistant","request_id":"r2","message":{"content":[{"type":"tool_use","id":"b","name":"mcp__arno__arno_read_range","input":{"path":"args.go"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"b","content":[{"type":"text","text":"r1 · args.go:1-8\n\tif len(args) == 0 {\n\t\treturn args[:0]\n"}]}]}}`,
		`{"type":"assistant","request_id":"r3","message":{"content":[{"type":"tool_use","id":"c","name":"Read","input":{"file_path":"/x/workspace/flag_args.go"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"c","content":"package main"}]}}`,
	}
	path := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gold := &Gold{Files: []string{"args.go", "other.go"}, Lines: []string{"if len(args) == 0 {", "return args[:0]", "return append([]string(nil), args...)"}}
	in, err := Analyze(RunResult{Arm: ArmArnoShell, Transcript: path, Gold: gold})
	if err != nil {
		t.Fatal(err)
	}
	if in.Yield == nil {
		t.Fatal("expected a yield with a gold set")
	}
	y := *in.Yield
	if y.GoldFiles != 2 || y.GoldFilesSeen != 2 || y.GoldLines != 3 || y.GoldLinesSeen != 2 || y.FirstGoldCall != 2 {
		t.Fatalf("unexpected yield %+v", y)
	}
	if y.ObservationTokens != in.ResultBytes/4 || y.LinesPerKToken() <= 0 || y.LineRecall() < 0.66 || y.LineRecall() > 0.67 || y.FileRecall() != 1 {
		t.Fatalf("unexpected derived figures %+v (result bytes %d)", y, in.ResultBytes)
	}

	without, err := Analyze(RunResult{Arm: ArmArnoShell, Transcript: path})
	if err != nil || without.Yield != nil {
		t.Fatalf("a run without a gold set has no yield, got %+v, %v", without.Yield, err)
	}

	// A shell run is valid without an ARNO init event; an ARNO arm would be
	// dropped from the report as not connected.
	report := InsightReport([]RunResult{{Arm: ArmShell, Transcript: path, Gold: gold}})
	if !strings.Contains(report, "yield:") || !strings.Contains(report, "files 100%") || !strings.Contains(report, "lines  67%") {
		t.Fatalf("expected the yield section in the report, got:\n%s", report)
	}
}

package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fixtures are real Claude Code stream-json transcripts, captured with a
// one-call shell run and a one-call Arno run, with local paths and settings
// removed.

func TestAnalyzeReadsARealShellTranscript(t *testing.T) {
	in, err := Analyze(RunResult{Arm: ArmShell, Transcript: "testdata/stream-shell.jsonl"})
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Calls) != 1 || in.Calls[0].Tool != "Bash" || in.Calls[0].Category != CategorySearch || in.Calls[0].Target != "ls" {
		t.Fatalf("expected one Bash ls search call, got %+v", in.Calls)
	}
	if in.Calls[0].ResultBytes == 0 {
		t.Error("the tool result's size was not joined to its call")
	}
	if in.Requests < 2 || in.PeakContextTokens == 0 || in.OutputTokens == 0 {
		t.Errorf("expected round trips and token usage, got %+v", in)
	}
	if !in.Valid() || in.ArnoStatus != "" {
		t.Errorf("a shell run is valid without Arno: %+v", in)
	}
}

func TestAnalyzeReadsARealArnoTranscript(t *testing.T) {
	in, err := Analyze(RunResult{Arm: ArmArno, Transcript: "testdata/stream-arno.jsonl"})
	if err != nil {
		t.Fatal(err)
	}
	if in.ArnoStatus != "connected" || !in.Valid() {
		t.Errorf("expected Arno connected, got %q", in.ArnoStatus)
	}
	if len(in.Calls) != 1 || in.Calls[0].Tool != "arno.outline" || in.Calls[0].Category != CategoryRead || in.Calls[0].Target != "main.go" {
		t.Fatalf("expected one arno.outline read of main.go, got %+v", in.Calls)
	}
	if in.Calls[0].ResultBytes == 0 {
		t.Error("the Arno result's size was not joined to its call")
	}
}

func TestAArnoArmWithoutArnoIsInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"system","subtype":"init","mcp_servers":[{"name":"arno","status":"failed"}]}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in, err := Analyze(RunResult{Arm: ArmArno, Transcript: path})
	if err != nil {
		t.Fatal(err)
	}
	if in.Valid() {
		t.Fatal("a Arno arm whose server failed must not count as a Arno result")
	}
}

// shell-lean has no MCP server at all, so it must not be read as a Arno arm
// whose server failed to connect.
func TestAShellLeanArmIsValidWithoutArno(t *testing.T) {
	if !(Insight{Arm: ArmShellLean}).Valid() {
		t.Fatal("a shell-lean run was counted as invalid for lacking Arno")
	}
}

// Exploration before the first edit, re-reads of an unchanged target, parallel
// calls and errors are derived from the call sequence.
func TestAnalyzeDerivesTheCallSequenceMetrics(t *testing.T) {
	lines := []string{
		`{"type":"assistant","request_id":"r1","message":{"content":[{"type":"tool_use","id":"a","name":"Read","input":{"file_path":"/x/workspace/main.go"}}],"usage":{"input_tokens":10,"cache_read_input_tokens":1000,"output_tokens":5}}}`,
		`{"type":"assistant","request_id":"r1","message":{"content":[{"type":"tool_use","id":"b","name":"mcp__arno__arno_find","input":{"query":"Run"}}],"usage":{"input_tokens":10,"cache_read_input_tokens":1000,"output_tokens":9}}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"a","content":"package main"}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"b","content":[{"type":"text","text":"not found"}],"is_error":true}]}}`,
		`{"type":"assistant","request_id":"r2","message":{"content":[{"type":"tool_use","id":"c","name":"Read","input":{"file_path":"/x/workspace/main.go"}}],"usage":{"input_tokens":10,"cache_read_input_tokens":2000,"output_tokens":3}}}`,
		`{"type":"assistant","request_id":"r3","message":{"content":[{"type":"tool_use","id":"d","name":"mcp__arno__arno_replace_text","input":{"path":"main.go","oldText":"a","newText":"b"}}]}}`,
		`{"type":"assistant","request_id":"r4","message":{"content":[{"type":"tool_use","id":"e","name":"Read","input":{"file_path":"/x/workspace/main.go"}}]}}`,
		`{"type":"assistant","request_id":"r5","message":{"content":[{"type":"tool_use","id":"f","name":"Bash","input":{"command":"cd /x/workspace && go test ./... 2>&1 | tail -5"}}]}}`,
	}
	path := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in, err := Analyze(RunResult{Arm: ArmArnoShell, Transcript: path})
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string][2]int{
		"requests":                {in.Requests, 5},
		"max parallel":            {in.MaxParallel, 2},
		"peak context":            {in.PeakContextTokens, 2010},
		"output tokens":           {in.OutputTokens, 9 + 3},
		"calls before first edit": {in.CallsBeforeFirstEdit, 3},
		"edits":                   {in.Edits, 1},
		"verify runs":             {in.VerifyRuns, 1},
		"repeated reads":          {in.RepeatedReads, 1},
		"tool errors":             {in.ToolErrors, 1},
		"result bytes":            {in.ResultBytes, len("package main") + len("not found")},
	}
	for name, pair := range checks {
		if pair[0] != pair[1] {
			t.Errorf("%s: got %d, want %d", name, pair[0], pair[1])
		}
	}
	if in.Calls[1].Tool != "arno.find" || in.Calls[1].Category != CategorySearch {
		t.Errorf("arno tool names should normalise: %+v", in.Calls[1])
	}
}

func TestClassifyCommand(t *testing.T) {
	cases := map[string]string{
		"go test ./... -run TestX":            CategoryVerify,
		"cd /w && npx ava test/main.ts":       CategoryVerify,
		".venv/bin/pytest -q tests/test_x.py": CategoryVerify,
		"cargo test --test integration":       CategoryVerify,
		"grep -rn 'func X' .":                 CategorySearch,
		"rg foo | head -20":                   CategorySearch,
		"cat main.go":                         CategoryRead,
		"sed -n '10,40p' main.go":             CategoryRead,
		"sed -i 's/a/b/' main.go":             CategoryEdit,
		"cat > new.go <<'EOF'":                CategoryEdit,
		"ls 2>&1":                             CategorySearch,
		"git diff":                            CategoryGit,
		"cd /w && git status":                 CategoryGit,
		"echo hello":                          CategoryOther,
	}
	for command, want := range cases {
		if got := classifyCommand(command); got != want {
			t.Errorf("%q: got %s, want %s", command, got, want)
		}
	}
}

func TestInsightReportComparesArmsPerTask(t *testing.T) {
	results := []RunResult{
		{Repository: "demo", Task: "t1", Arm: ArmShell, Repeat: 1, Success: true, Turns: 10, Transcript: "testdata/stream-shell.jsonl"},
		{Repository: "demo", Task: "t1", Arm: ArmArno, Repeat: 1, Success: true, Turns: 20, Transcript: "testdata/stream-arno.jsonl"},
		{Repository: "demo", Task: "t1", Arm: ArmArno, Repeat: 2, Success: false, Turns: 30, Transcript: "testdata/stream-arno.jsonl"},
		{Repository: "demo", Task: "t1", Arm: ArmShell, Repeat: 2},
	}
	report := InsightReport(results)
	for _, want := range []string{
		"3 runs analysed", "1 without a readable transcript", "tok k", "tokens per run by kind",
		"demo/t1", "shell", "arno", "(20–30)",
		"tools used by the Arno arms", "arno.outline",
		"largest tool results",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("expected %q in:\n%s", want, report)
		}
	}
}

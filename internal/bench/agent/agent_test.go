package agent

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeClaude stands in for the agent: it logs its arguments, makes a change in
// the workspace, and prints a result like `claude -p --output-format json`.
const fakeClaude = `#!/bin/sh
{ echo "=== run"; for arg in "$@"; do printf '[%s]\n' "$arg"; done; } >> "$FAKE_ARGS_LOG"
echo "[history $(git rev-list --all --count)]" >> "$FAKE_ARGS_LOG"
echo solved > answer.txt
echo tampered > check.txt
printf '{"type":"result","is_error":false,"num_turns":3,"total_cost_usd":%s,"duration_ms":1200,"usage":{"input_tokens":100,"output_tokens":50,"cache_creation_input_tokens":1000,"cache_read_input_tokens":2000},"permission_denials":[]}\n' "${FAKE_COST:-0.25}"
`

func setup(t *testing.T) (repo string, claude string, argsLog string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	repo = filepath.Join(dir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "bench@example.com"},
		{"config", "user.name", "Bench"},
	} {
		run(t, repo, "git", args...)
	}
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, repo, "git", "add", "-A")
	run(t, repo, "git", "commit", "-q", "-m", "initial")

	claude = filepath.Join(dir, "claude")
	if err := os.WriteFile(claude, []byte(fakeClaude), 0o755); err != nil {
		t.Fatal(err)
	}
	argsLog = filepath.Join(dir, "args.log")
	t.Setenv("FAKE_ARGS_LOG", argsLog)
	return repo, claude, argsLog
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v: %s", name, args, err, out)
	}
}

func taskFile() TaskFile {
	return TaskFile{
		Repository: Repository{Name: "demo", Language: "go"},
		Tasks: []Task{{
			ID:     "write-answer",
			Prompt: "Create answer.txt.",
			Verify: "test -f answer.txt",
		}},
	}
}

func TestRunMeasuresEveryArmAndVerifiesIndependently(t *testing.T) {
	repo, claude, argsLog := setup(t)
	var out bytes.Buffer

	results, err := Run(context.Background(), Config{
		Repo: repo, Tasks: taskFile(), Arms: []Arm{ArmShell, ArmJade, ArmJadeShell},
		Model: "sonnet", BudgetUSD: 5, PerRunUSD: 1, JadeMCP: "/opt/jade-mcp", Claude: claude, Out: &out,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected one result per arm, got %d", len(results))
	}
	for _, result := range results {
		if !result.Success || result.AgentError != "" {
			t.Errorf("%s: expected a verified success, got %+v", result.Arm, result)
		}
		if result.CostUSD != 0.25 || result.Turns != 3 || result.TotalTokens() != 3150 {
			t.Errorf("%s: result fields not parsed: %+v", result.Arm, result)
		}
		if result.LinesChanged != 2 || strings.Join(result.FilesChanged, ",") != "answer.txt,check.txt" {
			t.Errorf("%s: expected the new files counted as the diff, got %d lines %v", result.Arm, result.LinesChanged, result.FilesChanged)
		}
	}
	if lines := strings.Count(out.String(), "\n"); lines != 3 {
		t.Errorf("expected one JSON line per run, got %d", lines)
	}

	// The checkout the benchmark was pointed at is never modified.
	if _, err := os.Stat(filepath.Join(repo, "answer.txt")); err == nil {
		t.Error("a run modified the source checkout")
	}

	logData, _ := os.ReadFile(argsLog)
	runs := strings.Split(string(logData), "=== run\n")[1:]
	if len(runs) != 3 {
		t.Fatalf("expected three agent invocations, got %d", len(runs))
	}
	shell, jade, both := runs[0], runs[1], runs[2]
	if strings.Contains(shell, "[--mcp-config]") || !strings.Contains(shell, "[--tools]\n[default]") {
		t.Errorf("shell arm should have built-in tools and no MCP:\n%s", shell)
	}
	if !strings.Contains(jade, "[--tools]\n[]") || !strings.Contains(jade, "[--mcp-config]") {
		t.Errorf("jade arm should have no built-in tools and Jade's MCP server:\n%s", jade)
	}
	if !strings.Contains(both, "[--tools]\n[default]") || !strings.Contains(both, "[--mcp-config]") {
		t.Errorf("jade+shell arm should have both:\n%s", both)
	}
	for _, invocation := range runs {
		for _, shared := range []string{"[--strict-mcp-config]", "[--model]\n[sonnet]", "[--max-budget-usd]\n[1.00]", "[--setting-sources]\n[project]"} {
			if !strings.Contains(invocation, shared) {
				t.Errorf("every arm needs %s:\n%s", shared, invocation)
			}
		}
	}
}

// The agent must not find the fix in git history, dependency installs must not
// count as its diff, and the hidden test decides — even over a file of the
// same name the agent wrote.
func TestWorkspaceHidesHistoryAndHiddenTestsDecide(t *testing.T) {
	repo, claude, argsLog := setup(t)
	base, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "future.txt"), []byte("the fix\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, repo, "git", "add", "-A")
	run(t, repo, "git", "commit", "-q", "-m", "the fix")

	tasks := taskFile()
	tasks.Repository.Setup = "echo installed > setup.log"
	tasks.Tasks[0].Commit = strings.TrimSpace(string(base))
	tasks.Tasks[0].Verify = "grep -q hidden check.txt && test -f answer.txt && test ! -f future.txt"
	tasks.Tasks[0].VerifyFiles = map[string]string{"check.txt": "hidden\n"}

	results, err := Run(context.Background(), Config{
		Repo: repo, Tasks: tasks, Arms: []Arm{ArmShell}, BudgetUSD: 5, PerRunUSD: 1, Claude: claude,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	result := results[0]
	if !result.Success {
		t.Fatalf("expected the hidden test to pass, got %+v", result)
	}
	if strings.Join(result.FilesChanged, ",") != "answer.txt,check.txt" {
		t.Errorf("expected only the agent's files in the diff, not setup output, got %v", result.FilesChanged)
	}

	logData, _ := os.ReadFile(argsLog)
	if !strings.Contains(string(logData), "[history 1]") {
		t.Errorf("the agent should see exactly one baseline commit, log:\n%s", logData)
	}

	bad := taskFile()
	bad.Tasks[0].VerifyFiles = map[string]string{"../escape.txt": "x"}
	results, _ = Run(context.Background(), Config{
		Repo: repo, Tasks: bad, Arms: []Arm{ArmShell}, BudgetUSD: 5, PerRunUSD: 1, Claude: claude,
	})
	if results[0].Success || !strings.Contains(results[0].VerifyOutput, "outside the workspace") {
		t.Errorf("a verify file outside the workspace must be refused, got %+v", results[0])
	}
}

func TestRunStopsBeforeExceedingTheBudget(t *testing.T) {
	repo, claude, _ := setup(t)
	t.Setenv("FAKE_COST", "0.40")

	results, err := Run(context.Background(), Config{
		Repo: repo, Tasks: taskFile(), Arms: []Arm{ArmShell, ArmJade, ArmJadeShell},
		BudgetUSD: 1.0, PerRunUSD: 0.5, Repeats: 5, JadeMCP: "/opt/jade-mcp", Claude: claude,
	})
	if !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("expected the budget to stop the run, got %v", err)
	}
	spent := 0.0
	for _, result := range results {
		spent += result.CostUSD
	}
	if len(results) != 3 || spent > 1.2+1e-9 {
		t.Fatalf("expected three runs and at most one partial overrun, got %d runs costing $%.2f", len(results), spent)
	}
}

func TestAgentThatPrintsNoResultIsChargedItsCap(t *testing.T) {
	repo, _, _ := setup(t)
	broken := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(broken, []byte("#!/bin/sh\necho boom >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	results, _ := Run(context.Background(), Config{
		Repo: repo, Tasks: taskFile(), Arms: []Arm{ArmShell}, BudgetUSD: 5, PerRunUSD: 0.75, Claude: broken,
	})
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}
	result := results[0]
	if result.Success || result.AgentError == "" || !result.CostEstimated || result.CostUSD != 0.75 {
		t.Fatalf("a run with no result should fail, say why, and be charged its cap: %+v", result)
	}
}

func TestScoreAppliesTheFixedScorecard(t *testing.T) {
	shell := ArmSummary{Arm: ArmShell, SuccessRate: 0.60, MeanTokens: 1000}
	cases := []struct {
		name      string
		candidate ArmSummary
		pass      bool
	}{
		{"cheaper and as good", ArmSummary{Arm: ArmJade, SuccessRate: 0.60, MeanTokens: 900}, true},
		{"less successful however cheap", ArmSummary{Arm: ArmJade, SuccessRate: 0.55, MeanTokens: 500}, false},
		{"costlier with no gain", ArmSummary{Arm: ArmJade, SuccessRate: 0.60, MeanTokens: 1050}, false},
		{"+10% tokens for +5 points", ArmSummary{Arm: ArmJade, SuccessRate: 0.65, MeanTokens: 1100}, true},
		{"+20% tokens for +9 points", ArmSummary{Arm: ArmJade, SuccessRate: 0.69, MeanTokens: 1200}, false},
		{"+20% tokens for +10 points", ArmSummary{Arm: ArmJade, SuccessRate: 0.70, MeanTokens: 1200}, true},
	}
	for _, tc := range cases {
		if got := Score(shell, tc.candidate); got.Pass != tc.pass {
			t.Errorf("%s: pass=%v, want %v (%v)", tc.name, got.Pass, tc.pass, got.Reasons)
		}
	}
}

func TestLoadTaskFileRejectsIncompleteTasks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	if err := os.WriteFile(path, []byte(`{"repository":{"name":"x"},"tasks":[{"id":"a","prompt":"do it"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTaskFile(path); err == nil || !strings.Contains(err.Error(), "verify") {
		t.Fatalf("a task without a verify command must be rejected, got %v", err)
	}
}

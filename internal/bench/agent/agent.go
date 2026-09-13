// Package agent runs the release plan's external benchmark: a real agent
// (Claude Code headless) solving real tasks in a repository Jade was not built
// in, once per arm — shell tools only, Jade only, and Jade plus shell.
//
// internal/bench measures what one answer costs with no agent in the loop.
// This package measures what that cannot: whether the task got done, and what
// the agent spent getting there. Success is decided by the task's own verify
// command run after the agent stops, never by what the agent says about its
// work.
//
// Adding a repository is a task file, not code (docs/benchmark.md).
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/julianbei/jade/internal/pathguard"
)

// Arm is one configuration of the agent's tools.
type Arm string

const (
	// ArmShell is the agent's built-in tools and no MCP server: the baseline.
	ArmShell Arm = "shell"
	// ArmJade is Jade's tools and nothing else.
	ArmJade Arm = "jade"
	// ArmJadeShell is both. The plan expects this may be the practical winner.
	ArmJadeShell Arm = "jade+shell"
)

// ParseArms reads a comma-separated arm list.
func ParseArms(list string) ([]Arm, error) {
	var arms []Arm
	for _, name := range strings.Split(list, ",") {
		switch arm := Arm(strings.TrimSpace(name)); arm {
		case ArmShell, ArmJade, ArmJadeShell:
			arms = append(arms, arm)
		case "":
		default:
			return nil, fmt.Errorf("unknown arm %q (want shell, jade or jade+shell)", name)
		}
	}
	if len(arms) == 0 {
		return nil, fmt.Errorf("no arms given")
	}
	return arms, nil
}

// TaskFile is one repository and the tasks asked of it.
type TaskFile struct {
	Repository Repository `json:"repository"`
	Tasks      []Task     `json:"tasks"`
}

// Repository records where the tasks come from. The local checkout is given
// on the command line; URL and Commit are provenance, and Commit pins every
// run to the same starting state.
type Repository struct {
	Name     string `json:"name"`
	Language string `json:"language"`
	URL      string `json:"url"`
	Commit   string `json:"commit"`
	// Setup runs after checkout and before the agent starts, e.g. installing
	// dependencies. What it produces joins the baseline, so it never counts
	// as the agent's diff.
	Setup string `json:"setup"`
}

// Task is one piece of work. Verify is a shell command run in the workspace
// after the agent stops; exit 0 is success.
type Task struct {
	ID             string `json:"id"`
	Prompt         string `json:"prompt"`
	Verify         string `json:"verify"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
	// Commit overrides the repository's commit for this task: tasks taken
	// from different fixes start from different parents.
	Commit string `json:"commit"`
	// VerifyFiles are the hidden tests, written into the workspace after the
	// agent stops and before Verify runs. The agent never sees them, and any
	// file of the same name it wrote is overwritten.
	VerifyFiles map[string]string `json:"verifyFiles"`
	// VerifyFrom and VerifyPaths name hidden tests by reference: each path is
	// read at VerifyFrom (usually the fix commit) from the source checkout at
	// run time. Task files then carry no copy of the repository's code.
	VerifyFrom  string   `json:"verifyFrom"`
	VerifyPaths []string `json:"verifyPaths"`
}

// LoadTaskFile reads and checks a task file.
func LoadTaskFile(path string) (TaskFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return TaskFile{}, err
	}
	var file TaskFile
	if err := json.Unmarshal(data, &file); err != nil {
		return TaskFile{}, fmt.Errorf("%s: %w", path, err)
	}
	if file.Repository.Name == "" {
		return TaskFile{}, fmt.Errorf("%s: repository.name is required", path)
	}
	if len(file.Tasks) == 0 {
		return TaskFile{}, fmt.Errorf("%s: no tasks", path)
	}
	for i, task := range file.Tasks {
		if task.ID == "" || task.Prompt == "" || task.Verify == "" {
			return TaskFile{}, fmt.Errorf("%s: task %d needs id, prompt and verify", path, i+1)
		}
		if len(task.VerifyPaths) > 0 && task.VerifyFrom == "" {
			return TaskFile{}, fmt.Errorf("%s: task %s lists verifyPaths without verifyFrom", path, task.ID)
		}
	}
	return file, nil
}

// Config is one benchmark invocation.
type Config struct {
	// Repo is a local git checkout of the task file's repository. Each run
	// clones it, so the checkout itself is never modified.
	Repo  string
	Tasks TaskFile
	Arms  []Arm
	// Model is passed to the agent, e.g. "sonnet".
	Model string
	// BudgetUSD stops the benchmark before a run once this much is spent.
	BudgetUSD float64
	// PerRunUSD caps each agent run.
	PerRunUSD float64
	Repeats   int
	// JadeMCP is the jade-mcp binary for the Jade arms.
	JadeMCP string
	// Claude is the agent binary; "claude" when empty.
	Claude string
	// WorkDir holds the per-run clones; the system temp directory when empty.
	WorkDir string
	// Env is added to the agent's environment.
	Env []string
	// Out receives one JSON line per run as it finishes, so a benchmark cut
	// short still leaves its results.
	Out io.Writer
}

// ErrBudgetExhausted stops a benchmark before a run that could exceed the
// budget.
var ErrBudgetExhausted = errors.New("benchmark budget exhausted")

// RunResult is one agent run, measured.
type RunResult struct {
	Repository string `json:"repository"`
	Language   string `json:"language"`
	Task       string `json:"task"`
	Arm        Arm    `json:"arm"`
	Repeat     int    `json:"repeat"`

	Success      bool   `json:"success"`
	VerifyOutput string `json:"verifyOutput,omitempty"`
	AgentError   string `json:"agentError,omitempty"`

	CostUSD           float64 `json:"costUSD"`
	CostEstimated     bool    `json:"costEstimated,omitempty"`
	Turns             int     `json:"turns"`
	InputTokens       int     `json:"inputTokens"`
	OutputTokens      int     `json:"outputTokens"`
	CacheWriteTokens  int     `json:"cacheWriteTokens"`
	CacheReadTokens   int     `json:"cacheReadTokens"`
	DurationMS        int64   `json:"durationMS"`
	PermissionDenials int     `json:"permissionDenials"`

	LinesChanged int      `json:"linesChanged"`
	FilesChanged []string `json:"filesChanged,omitempty"`
}

// TotalTokens is every token the run consumed, cached or not.
func (r RunResult) TotalTokens() int {
	return r.InputTokens + r.OutputTokens + r.CacheWriteTokens + r.CacheReadTokens
}

// Run executes every task on every arm, repeat by repeat. Arms are interleaved
// per task so drift over a long run (server load, model changes) lands on all
// arms alike rather than on whichever ran last.
func Run(ctx context.Context, cfg Config) ([]RunResult, error) {
	if cfg.Repeats < 1 {
		cfg.Repeats = 1
	}
	if cfg.Claude == "" {
		cfg.Claude = "claude"
	}

	var results []RunResult
	spent := 0.0
	for repeat := 1; repeat <= cfg.Repeats; repeat++ {
		for _, task := range cfg.Tasks.Tasks {
			for _, arm := range cfg.Arms {
				remaining := cfg.BudgetUSD - spent
				if remaining <= 0 || (cfg.PerRunUSD > 0 && remaining < cfg.PerRunUSD/4) {
					return results, fmt.Errorf("%w: $%.2f of $%.2f spent", ErrBudgetExhausted, spent, cfg.BudgetUSD)
				}
				cap := cfg.PerRunUSD
				if cap <= 0 || cap > remaining {
					cap = remaining
				}

				result := runOne(ctx, cfg, task, arm, repeat, cap)
				spent += result.CostUSD
				results = append(results, result)
				if cfg.Out != nil {
					if line, err := json.Marshal(result); err == nil {
						_, _ = cfg.Out.Write(append(line, '\n'))
					}
				}
				if ctx.Err() != nil {
					return results, ctx.Err()
				}
			}
		}
	}
	return results, nil
}

const (
	defaultTaskTimeout = 15 * time.Minute
	setupTimeout       = 15 * time.Minute
	verifyTimeout      = 5 * time.Minute
	maxVerifyOutput    = 2000
)

func runOne(ctx context.Context, cfg Config, task Task, arm Arm, repeat int, capUSD float64) RunResult {
	result := RunResult{
		Repository: cfg.Tasks.Repository.Name,
		Language:   cfg.Tasks.Repository.Language,
		Task:       task.ID,
		Arm:        arm,
		Repeat:     repeat,
	}

	parent, err := os.MkdirTemp(cfg.WorkDir, "jade-bench-*")
	if err != nil {
		result.AgentError = err.Error()
		return result
	}
	defer os.RemoveAll(parent)

	workspace := filepath.Join(parent, "workspace")
	commit := task.Commit
	if commit == "" {
		commit = cfg.Tasks.Repository.Commit
	}
	if err := prepareWorkspace(ctx, cfg.Repo, commit, cfg.Tasks.Repository.Setup, workspace); err != nil {
		result.AgentError = err.Error()
		return result
	}

	mcpConfig := ""
	if arm != ArmShell {
		mcpConfig = filepath.Join(parent, "mcp.json")
		if err := writeMCPConfig(mcpConfig, cfg.JadeMCP, workspace); err != nil {
			result.AgentError = err.Error()
			return result
		}
	}

	timeout := defaultTaskTimeout
	if task.TimeoutSeconds > 0 {
		timeout = time.Duration(task.TimeoutSeconds) * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, cfg.Claude, AgentArgs(task.Prompt, arm, cfg.Model, capUSD, mcpConfig)...)
	cmd.Dir = workspace
	cmd.Env = append(os.Environ(), cfg.Env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	started := time.Now()
	runErr := cmd.Run()

	parsed, parseErr := parseResult(stdout.Bytes())
	switch {
	case parseErr == nil:
		result.CostUSD = parsed.TotalCostUSD
		result.Turns = parsed.NumTurns
		result.InputTokens = parsed.Usage.InputTokens
		result.OutputTokens = parsed.Usage.OutputTokens
		result.CacheWriteTokens = parsed.Usage.CacheCreationInputTokens
		result.CacheReadTokens = parsed.Usage.CacheReadInputTokens
		result.DurationMS = parsed.DurationMS
		result.PermissionDenials = len(parsed.PermissionDenials)
		if parsed.IsError {
			result.AgentError = "agent reported an error: " + parsed.Subtype
		}
	default:
		// A run that died without a result still spent money. Charging the
		// cap keeps the budget honest; the flag says the number is not real.
		result.CostUSD = capUSD
		result.CostEstimated = true
		result.DurationMS = time.Since(started).Milliseconds()
		result.AgentError = strings.TrimSpace(fmt.Sprintf("%v %s", runErr, tail(stderr.String(), 500)))
	}

	// The diff is measured before the hidden tests are written, so it is
	// exactly what the agent left.
	result.LinesChanged, result.FilesChanged = diffStats(workspace)
	if hidden, err := hiddenFiles(cfg.Repo, task); err != nil {
		result.VerifyOutput = err.Error()
	} else if err := writeVerifyFiles(workspace, hidden); err != nil {
		result.VerifyOutput = err.Error()
	} else {
		result.Success, result.VerifyOutput = verify(ctx, workspace, task.Verify)
	}
	return result
}

// AgentArgs builds the headless invocation for one arm. Every arm gets the
// same model, budget, permission mode and settings; only tools and MCP differ.
//
// --strict-mcp-config keeps the operator's own MCP servers out of every arm,
// and --setting-sources project keeps user-level hooks and plugins out, so the
// arms differ in exactly one thing.
func AgentArgs(prompt string, arm Arm, model string, capUSD float64, mcpConfig string) []string {
	args := []string{
		"-p", prompt,
		"--output-format", "json",
		"--no-session-persistence",
		"--permission-mode", "bypassPermissions",
		"--setting-sources", "project",
		"--strict-mcp-config",
		"--max-budget-usd", strconv.FormatFloat(capUSD, 'f', 2, 64),
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	switch arm {
	case ArmShell:
		args = append(args, "--tools", "default")
	case ArmJade:
		args = append(args, "--tools", "", "--mcp-config", mcpConfig)
	case ArmJadeShell:
		args = append(args, "--tools", "default", "--mcp-config", mcpConfig)
	}
	return args
}

type claudeResult struct {
	IsError      bool    `json:"is_error"`
	Subtype      string  `json:"subtype"`
	NumTurns     int     `json:"num_turns"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	DurationMS   int64   `json:"duration_ms"`
	Usage        struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	} `json:"usage"`
	PermissionDenials []json.RawMessage `json:"permission_denials"`
}

// parseResult reads the last JSON object the agent printed.
func parseResult(output []byte) (claudeResult, error) {
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var parsed claudeResult
		if err := json.Unmarshal([]byte(line), &parsed); err == nil {
			return parsed, nil
		}
	}
	return claudeResult{}, fmt.Errorf("no result JSON in agent output")
}

func checkout(repo string, commit string, dir string) error {
	if out, err := exec.Command("git", "clone", "--quiet", "--no-hardlinks", repo, dir).CombinedOutput(); err != nil {
		return fmt.Errorf("clone %s: %v: %s", repo, err, tail(string(out), 300))
	}
	if commit != "" {
		if out, err := exec.Command("git", "-C", dir, "checkout", "--quiet", commit).CombinedOutput(); err != nil {
			return fmt.Errorf("checkout %s: %v: %s", commit, err, tail(string(out), 300))
		}
	}
	return nil
}

// prepareWorkspace clones the checkout at commit, runs setup, and replaces the
// history with one baseline commit.
//
// The history goes because the fix a task was taken from is usually in it: an
// agent running `git log --all` would find the answer. Setup output joins the
// baseline, so installed dependencies never count as the agent's diff.
func prepareWorkspace(ctx context.Context, repo string, commit string, setup string, dir string) error {
	if err := checkout(repo, commit, dir); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(dir, ".git")); err != nil {
		return err
	}
	if strings.TrimSpace(setup) != "" {
		setupCtx, cancel := context.WithTimeout(ctx, setupTimeout)
		defer cancel()
		cmd := exec.CommandContext(setupCtx, "sh", "-c", setup)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("setup failed: %v: %s", err, tail(string(out), 500))
		}
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "-A"},
		{"-c", "user.name=jade-bench", "-c", "user.email=bench@localhost", "commit", "-q", "--no-verify", "--allow-empty", "-m", "baseline"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			return fmt.Errorf("baseline git %v: %v: %s", args, err, tail(string(out), 300))
		}
	}
	return nil
}

// hiddenFiles collects a task's hidden tests: inline VerifyFiles, plus each of
// VerifyPaths read at VerifyFrom from the source checkout.
func hiddenFiles(repo string, task Task) (map[string]string, error) {
	files := make(map[string]string, len(task.VerifyFiles)+len(task.VerifyPaths))
	for path, content := range task.VerifyFiles {
		files[path] = content
	}
	for _, path := range task.VerifyPaths {
		out, err := exec.Command("git", "-C", repo, "show", task.VerifyFrom+":"+path).Output()
		if err != nil {
			return nil, fmt.Errorf("hidden test %s at %s: %v", path, task.VerifyFrom, err)
		}
		files[path] = string(out)
	}
	return files, nil
}

// writeVerifyFiles writes the hidden tests, refusing any path that would land
// outside the workspace.
func writeVerifyFiles(dir string, files map[string]string) error {
	for rel, content := range files {
		absolute, err := pathguard.Resolve(dir, rel)
		if err != nil {
			return fmt.Errorf("verify file: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(absolute, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func writeMCPConfig(path string, jadeMCP string, workspace string) error {
	if jadeMCP == "" {
		return fmt.Errorf("a Jade arm needs the jade-mcp binary")
	}
	config := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"jade": map[string]interface{}{
				"type":    "stdio",
				"command": jadeMCP,
				"args":    []string{"--root", workspace},
			},
		},
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func verify(ctx context.Context, dir string, command string) (bool, string) {
	verifyCtx, cancel := context.WithTimeout(ctx, verifyTimeout)
	defer cancel()
	cmd := exec.CommandContext(verifyCtx, "sh", "-c", command)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return err == nil, tail(string(out), maxVerifyOutput)
}

// diffStats counts lines added plus removed and the files touched, new files
// included. Jade's telemetry log is excluded through .git/info/exclude, so it
// never counts as a change the agent made.
func diffStats(dir string) (int, []string) {
	_ = exec.Command("git", "-C", dir, "add", "--all", "--intent-to-add").Run()
	out, err := exec.Command("git", "-C", dir, "diff", "--numstat").Output()
	if err != nil {
		return 0, nil
	}
	lines := 0
	var files []string
	for _, row := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(row)
		if len(fields) < 3 {
			continue
		}
		added, _ := strconv.Atoi(fields[0])
		removed, _ := strconv.Atoi(fields[1])
		lines += added + removed
		files = append(files, fields[2])
	}
	return lines, files
}

func tail(text string, max int) string {
	text = strings.TrimSpace(text)
	if len(text) <= max {
		return text
	}
	return "…" + text[len(text)-max:]
}

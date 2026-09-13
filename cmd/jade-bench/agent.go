package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/julianbei/jade/internal/bench/agent"
)

// sandboxEnv marks an environment built to run agents with permissions
// bypassed, such as the benchmark container.
const sandboxEnv = "JADE_BENCH_SANDBOX"

// runAgentReport is `jade-bench agent-report`: one table from the result lines
// of any number of `agent` invocations.
func runAgentReport(args []string) int {
	flags := flag.NewFlagSet("agent-report", flag.ContinueOnError)
	in := flags.String("in", "bench-results.jsonl", "result lines written by jade-bench agent")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	file, err := os.Open(*in)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer file.Close()
	results, err := agent.ReadResults(file)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	repositories := map[string]bool{}
	for _, result := range results {
		repositories[result.Repository] = true
	}
	fmt.Printf("%d runs across %d repositories\n\n", len(results), len(repositories))
	fmt.Print(agent.Report(results))
	fmt.Print(agent.ConfusionText(results))
	fmt.Print(agent.ToolUseText(results))
	return 0
}

// besideOut is the flag's directory, or one named after the results file.
func besideOut(flagValue string, out string, suffix string) string {
	path := flagValue
	if path == "" {
		path = out + suffix
	}
	if absolute, err := filepath.Abs(path); err == nil {
		return absolute
	}
	return path
}

// runAgent is `jade-bench agent`: the external benchmark with a real agent.
func runAgent(args []string) int {
	flags := flag.NewFlagSet("agent", flag.ContinueOnError)
	tasksPath := flags.String("tasks", "", "task file (JSON); see docs/benchmark.md")
	repo := flags.String("repo", "", "local git checkout of the task file's repository")
	arms := flags.String("arms", "shell,jade,jade+shell", "arms to run")
	model := flags.String("model", "sonnet", "agent model")
	budget := flags.Float64("budget", 50, "stop before a run once this many USD are spent")
	perRun := flags.Float64("per-run", 2, "USD cap for each agent run")
	repeats := flags.Int("repeats", 1, "times to run every task on every arm")
	jadeMCP := flags.String("jade-mcp", "jade-mcp", "jade-mcp binary for the Jade arms")
	out := flags.String("out", "bench-results.jsonl", "append one JSON line per run here")
	allowHost := flags.Bool("allow-host", false, "run agents with permissions bypassed on this machine, outside a sandbox")
	telemetryDir := flags.String("telemetry-dir", "", "keep each Jade-arm run's telemetry here (default: <out>.telemetry)")
	transcriptDir := flags.String("transcript-dir", "", "keep every run's agent transcript here (default: <out>.transcripts)")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	// Every arm runs with permissions bypassed so no run stalls on a prompt.
	// That is only acceptable where the agent cannot reach anything that
	// matters, so the host is refused unless the operator says otherwise.
	if os.Getenv(sandboxEnv) != "1" && !*allowHost {
		fmt.Fprintf(os.Stderr, "refusing to run agents with permissions bypassed outside a sandbox.\n"+
			"Run inside the benchmark container (%s=1), or pass -allow-host if you accept the risk.\n", sandboxEnv)
		return 2
	}
	if *tasksPath == "" || *repo == "" {
		fmt.Fprintln(os.Stderr, "usage: jade-bench agent -tasks <file.json> -repo <checkout> [flags]")
		return 2
	}

	file, err := agent.LoadTaskFile(*tasksPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	armList, err := agent.ParseArms(*arms)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	jadeBinary := ""
	if strings.Contains(*arms, "jade") {
		if jadeBinary, err = exec.LookPath(*jadeMCP); err != nil {
			fmt.Fprintf(os.Stderr, "jade-mcp not found: %v\n", err)
			return 2
		}
		if jadeBinary, err = filepath.Abs(jadeBinary); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}

	output, err := os.OpenFile(*out, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer output.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	results, runErr := agent.Run(ctx, agent.Config{
		Repo:      *repo,
		Tasks:     file,
		Arms:      armList,
		Model:     *model,
		BudgetUSD: *budget,
		PerRunUSD: *perRun,
		Repeats:   *repeats,
		JadeMCP:   jadeBinary,
		Out:       output,

		TelemetryDir:  besideOut(*telemetryDir, *out, ".telemetry"),
		TranscriptDir: besideOut(*transcriptDir, *out, ".transcripts"),
	})
	fmt.Printf("%s · %d tasks · %s\n\n", file.Repository.Name, len(file.Tasks), *model)
	fmt.Print(agent.Report(results))
	fmt.Print(agent.ConfusionText(results))

	switch {
	case runErr == nil:
		return 0
	case errors.Is(runErr, agent.ErrBudgetExhausted):
		fmt.Fprintf(os.Stderr, "\nstopped: %v\n", runErr)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "\nstopped: %v\n", runErr)
		return 1
	}
}

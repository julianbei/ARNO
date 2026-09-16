// Package bench measures what it costs an agent to answer a question with
// arno's tools versus with ordinary shell and file tools.
//
// # What this measures, and what it does not
//
// docs/scope.md §26 asks whether an agent using arno "completes real tasks more
// efficiently and reliably than shell/file tooling", and §42-43 lists
// tokens, turns and success rate as the metrics. This harness measures the
// first of those three, deterministically and without an LLM in the loop:
// for each scenario it runs both arms and counts the bytes each one puts in
// front of the agent — a "tokens to answer" metric rather than a
// search-precision score.
//
// It deliberately does NOT measure turns or success rate. Both require a
// real agent making real decisions, and faking them with a scripted call
// sequence would produce a number that looks like evidence while measuring
// only the script's author. Turn counts are reported as the number of tool
// calls each arm needs, which is a floor on turns, not a measurement of
// them.
//
// The honest reading of a result table is therefore: "answering this
// question costs N tokens of context through arno and M through shell."
// That is a real, reproducible comparison, and it is the part of docs/scope.md's
// question that can be answered without a benchmark agent.
package bench

import (
	"fmt"
	"sort"
	"strings"
)

// Arm is one way of answering a scenario's question. It returns everything
// the agent would have to read, concatenated, plus how many tool calls it
// took.
type Arm func() (output string, calls int, err error)

// Scenario is one question asked of a repository, answered two ways.
type Scenario struct {
	Name string
	// Question is the agent-level task, phrased as an agent would think of
	// it rather than as a tool invocation.
	Question string
	Arno     Arm
	Shell    Arm
}

// ArmResult is one arm's measured cost.
type ArmResult struct {
	Bytes  int
	Tokens int
	Calls  int
	Err    error
}

// Result is one scenario measured on both arms.
type Result struct {
	Name     string
	Question string
	Arno     ArmResult
	Shell    ArmResult
}

// TokenRatio is arno's token cost as a multiple of shell's. Below 1.0 means
// arno is cheaper. Returns 0 when shell produced nothing to compare against.
func (r Result) TokenRatio() float64 {
	if r.Shell.Tokens == 0 {
		return 0
	}
	return float64(r.Arno.Tokens) / float64(r.Shell.Tokens)
}

// EstimateTokens approximates tokens from bytes at the widely used ~4
// characters per token. It is an estimate, not a tokenizer: the comparison
// between two arms is the meaningful output, not either absolute number, and
// both arms are estimated identically so the ratio holds regardless of the
// constant's exact value.
func EstimateTokens(bytes int) int {
	if bytes <= 0 {
		return 0
	}
	return (bytes + 3) / 4
}

// Run measures every scenario. A failing arm is recorded rather than
// aborting the suite: one broken scenario should not hide the rest, and a
// tool that errors is itself a result worth seeing.
func Run(scenarios []Scenario) []Result {
	results := make([]Result, 0, len(scenarios))
	for _, scenario := range scenarios {
		results = append(results, Result{
			Name:     scenario.Name,
			Question: scenario.Question,
			Arno:     measure(scenario.Arno),
			Shell:    measure(scenario.Shell),
		})
	}
	return results
}

func measure(arm Arm) ArmResult {
	if arm == nil {
		return ArmResult{Err: fmt.Errorf("arm not implemented")}
	}
	output, calls, err := arm()
	return ArmResult{
		Bytes:  len(output),
		Tokens: EstimateTokens(len(output)),
		Calls:  calls,
		Err:    err,
	}
}

// Report renders results as plain text. Deliberately not JSON: this output
// is read by a person deciding whether arno is worth continuing, and it is
// also the house style Phase 10 is moving every arno response toward.
func Report(results []Result) string {
	var b strings.Builder

	b.WriteString("scenario                        arno      shell     ratio   calls (j/s)\n")
	b.WriteString("--------                        ----      -----     -----   -----------\n")

	var totalArno, totalShell int
	for _, r := range results {
		totalArno += r.Arno.Tokens
		totalShell += r.Shell.Tokens

		ratio := "n/a"
		if value := r.TokenRatio(); value > 0 {
			ratio = fmt.Sprintf("%.2fx", value)
		}

		b.WriteString(fmt.Sprintf("%-30s  %-9s %-9s %-7s %d/%d\n",
			truncate(r.Name, 30),
			tokensCell(r.Arno),
			tokensCell(r.Shell),
			ratio,
			r.Arno.Calls, r.Shell.Calls))
	}

	b.WriteString("--------------------------------------------------------------------\n")
	overall := "n/a"
	if totalShell > 0 {
		overall = fmt.Sprintf("%.2fx", float64(totalArno)/float64(totalShell))
	}
	b.WriteString(fmt.Sprintf("%-30s  %-9d %-9d %-7s\n", "TOTAL", totalArno, totalShell, overall))
	b.WriteString("\nTokens are estimated at ~4 chars/token and measure tool output only.\n")
	b.WriteString("Turns and success rate are NOT measured here — both need a real agent.\n")

	if errs := collectErrors(results); len(errs) > 0 {
		b.WriteString("\nerrors:\n")
		for _, line := range errs {
			b.WriteString("  " + line + "\n")
		}
	}

	return b.String()
}

func tokensCell(r ArmResult) string {
	if r.Err != nil {
		return "ERR"
	}
	return fmt.Sprintf("%d", r.Tokens)
}

func collectErrors(results []Result) []string {
	lines := make([]string, 0)
	for _, r := range results {
		if r.Arno.Err != nil {
			lines = append(lines, fmt.Sprintf("%s [arno]: %v", r.Name, r.Arno.Err))
		}
		if r.Shell.Err != nil {
			lines = append(lines, fmt.Sprintf("%s [shell]: %v", r.Name, r.Shell.Err))
		}
	}
	sort.Strings(lines)
	return lines
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

package agent

import (
	"fmt"
	"math"
	"strings"
)

// The scorecard, fixed on 2026-09-13 before any run and recorded in
// docs/benchmark.md. It is not to be tuned after results are seen.
//
// Non-negotiable against the shell arm: task success at least equal.
// Tradeable: tokens may exceed the shell arm's by tokenAllowancePerStep for
// every full successStepPoints of success gained over it.
const (
	tokenAllowancePerStep = 0.10
	successStepPoints     = 5.0
)

// ArmSummary aggregates one arm's runs.
type ArmSummary struct {
	Arm              Arm
	Runs             int
	Successes        int
	AgentErrors      int
	SuccessRate      float64
	MeanTokens       float64
	MeanTurns        float64
	MeanCostUSD      float64
	MeanSeconds      float64
	MeanLinesChanged float64
	MeanFilesChanged float64
}

// Summarize aggregates results per arm, in shell, ARNO, ARNO+shell order.
func Summarize(results []RunResult) []ArmSummary {
	var summaries []ArmSummary
	for _, arm := range []Arm{ArmShell, ArmArno, ArmArnoShell} {
		summary := ArmSummary{Arm: arm}
		for _, result := range results {
			if result.Arm != arm {
				continue
			}
			summary.Runs++
			if result.Success {
				summary.Successes++
			}
			if result.AgentError != "" {
				summary.AgentErrors++
			}
			summary.MeanTokens += float64(result.TotalTokens())
			summary.MeanTurns += float64(result.Turns)
			summary.MeanCostUSD += result.CostUSD
			summary.MeanSeconds += float64(result.DurationMS) / 1000
			summary.MeanLinesChanged += float64(result.LinesChanged)
			summary.MeanFilesChanged += float64(len(result.FilesChanged))
		}
		if summary.Runs == 0 {
			continue
		}
		n := float64(summary.Runs)
		summary.SuccessRate = float64(summary.Successes) / n
		summary.MeanTokens /= n
		summary.MeanTurns /= n
		summary.MeanCostUSD /= n
		summary.MeanSeconds /= n
		summary.MeanLinesChanged /= n
		summary.MeanFilesChanged /= n
		summaries = append(summaries, summary)
	}
	return summaries
}

// Verdict is one arm scored against the shell arm.
type Verdict struct {
	Arm     Arm
	Pass    bool
	Reasons []string
}

// Score applies the fixed scorecard.
func Score(shell ArmSummary, candidate ArmSummary) Verdict {
	verdict := Verdict{Arm: candidate.Arm, Pass: true}

	// Rounded before counting steps: 0.70-0.60 is 9.9999… points in floating
	// point, which would silently cost a whole allowance step.
	gain := math.Round((candidate.SuccessRate-shell.SuccessRate)*100*1e6) / 1e6
	if gain < 0 {
		verdict.Pass = false
		verdict.Reasons = append(verdict.Reasons, fmt.Sprintf("success %.0f%% is below shell's %.0f%%", candidate.SuccessRate*100, shell.SuccessRate*100))
	} else {
		verdict.Reasons = append(verdict.Reasons, fmt.Sprintf("success %.0f%% vs shell %.0f%%", candidate.SuccessRate*100, shell.SuccessRate*100))
	}

	if shell.MeanTokens > 0 {
		ratio := candidate.MeanTokens / shell.MeanTokens
		allowed := 1 + tokenAllowancePerStep*math.Floor(math.Max(gain, 0)/successStepPoints)
		if ratio > allowed+1e-9 {
			verdict.Pass = false
			verdict.Reasons = append(verdict.Reasons, fmt.Sprintf("tokens %.2fx shell, over the %.2fx allowed", ratio, allowed))
		} else {
			verdict.Reasons = append(verdict.Reasons, fmt.Sprintf("tokens %.2fx shell, within %.2fx", ratio, allowed))
		}
	}
	return verdict
}

// Report renders the per-arm table and the scorecard verdicts.
func Report(results []RunResult) string {
	summaries := Summarize(results)
	if len(summaries) == 0 {
		return "no runs\n"
	}

	var b strings.Builder
	b.WriteString("arm          runs  success  tokens     turns  cost    seconds  lines  files  errors\n")
	var shell *ArmSummary
	for i := range summaries {
		s := summaries[i]
		if s.Arm == ArmShell {
			shell = &summaries[i]
		}
		fmt.Fprintf(&b, "%-12s %4d  %6.0f%%  %9.0f  %5.1f  $%5.2f  %7.1f  %5.1f  %5.1f  %6d\n",
			s.Arm, s.Runs, s.SuccessRate*100, s.MeanTokens, s.MeanTurns, s.MeanCostUSD, s.MeanSeconds,
			s.MeanLinesChanged, s.MeanFilesChanged, s.AgentErrors)
	}

	if shell != nil {
		b.WriteString("\nscorecard (fixed 2026-09-13; success never below shell, +10% tokens per +5 points):\n")
		for _, s := range summaries {
			if s.Arm == ArmShell {
				continue
			}
			verdict := Score(*shell, s)
			word := "PASS"
			if !verdict.Pass {
				word = "FAIL"
			}
			fmt.Fprintf(&b, "  %s %s · %s\n", word, s.Arm, strings.Join(verdict.Reasons, " · "))
		}
	}
	b.WriteString("\nmeans per run. Invalid and unintended edits are not scored yet; lines and files are the diff the agent left.\n")
	return b.String()
}

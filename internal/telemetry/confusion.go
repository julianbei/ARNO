package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// Tool confusion is measured from call sequences, not from what a call
// contained. The signals of a wrong pick: an agent asks one inspect tool about
// a target and then asks a different one about the same target; it retries a
// tool straight after that tool said ambiguous or not_found; or it never calls
// a tool at all. Release plan Phase 3 merges and cuts tools from this report.

// targetKeys are the arguments that name what a call is about, most specific
// first.
var targetKeys = []string{"path", "symbolId", "symbolName", "query"}

// TargetOf returns a short hash of what a call is about, or "" when the call
// names nothing. A hash rather than the value keeps the log free of paths and
// names; it only has to tell "same target" from "different target".
func TargetOf(args map[string]interface{}) string {
	for _, key := range targetKeys {
		value, ok := args[key].(string)
		if !ok || strings.TrimSpace(value) == "" {
			continue
		}
		sum := sha256.Sum256([]byte(key + "\x00" + value))
		return hex.EncodeToString(sum[:])[:10]
	}
	return ""
}

// inspectTools is the cluster whose members answer overlapping questions.
// Switching between two of them on one target is the confusion signal; moving
// from a read to an edit on the same file is ordinary work and not counted.
var inspectTools = map[string]bool{
	"jade.find":           true,
	"jade.grep":           true,
	"jade.retrieve":       true,
	"jade.context":        true,
	"jade.read_range":     true,
	"jade.outline":        true,
	"jade.references":     true,
	"jade.workspace_tree": true,
	"jade.history":        true,
}

// ToolSwitch counts one inspect tool followed by a different one on the same
// target.
type ToolSwitch struct {
	From  string
	To    string
	Count int
}

// ToolRetry counts a tool called again right after it answered with a failure
// that means the caller picked the wrong input or the wrong tool.
type ToolRetry struct {
	Tool  string
	After Outcome
	Count int
}

// ConfusionReport is what Phase 3 reads to decide which tools to merge or cut.
type ConfusionReport struct {
	Switches    []ToolSwitch
	Retries     []ToolRetry
	NeverCalled []string
}

// AnalyzeConfusion reads records in log order. catalog is the full tool list;
// without it NeverCalled is empty.
func AnalyzeConfusion(records []Record, catalog []string) ConfusionReport {
	switches := map[[2]string]int{}
	retries := map[ToolRetry]int{}
	called := map[string]bool{}

	for i, record := range records {
		called[record.Tool] = true
		if i == 0 {
			continue
		}
		previous := records[i-1]

		if record.Target != "" && record.Target == previous.Target && record.Tool != previous.Tool &&
			inspectTools[record.Tool] && inspectTools[previous.Tool] {
			switches[[2]string{previous.Tool, record.Tool}]++
		}
		if record.Tool == previous.Tool && (previous.Outcome == Ambiguous || previous.Outcome == NotFound) {
			retries[ToolRetry{Tool: record.Tool, After: previous.Outcome}]++
		}
	}

	report := ConfusionReport{}
	for pair, count := range switches {
		report.Switches = append(report.Switches, ToolSwitch{From: pair[0], To: pair[1], Count: count})
	}
	sort.Slice(report.Switches, func(a, b int) bool {
		x, y := report.Switches[a], report.Switches[b]
		if x.Count != y.Count {
			return x.Count > y.Count
		}
		return x.From+x.To < y.From+y.To
	})

	for key, count := range retries {
		report.Retries = append(report.Retries, ToolRetry{Tool: key.Tool, After: key.After, Count: count})
	}
	sort.Slice(report.Retries, func(a, b int) bool {
		x, y := report.Retries[a], report.Retries[b]
		if x.Count != y.Count {
			return x.Count > y.Count
		}
		return x.Tool+string(x.After) < y.Tool+string(y.After)
	})

	for _, tool := range catalog {
		if !called[tool] {
			report.NeverCalled = append(report.NeverCalled, tool)
		}
	}
	sort.Strings(report.NeverCalled)
	return report
}

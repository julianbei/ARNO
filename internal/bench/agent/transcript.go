package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// RunToolUse is what one run's transcript says the agent did.
type RunToolUse struct {
	// Turns is the number of assistant messages; Calls the tool calls in
	// them, in order. A turn can carry several parallel calls, so the two
	// differ, and the gap is itself a finding.
	Turns int
	Calls []string
}

// ToolUse reads a stream-json transcript and lists its tool calls in order.
func ToolUse(path string) (RunToolUse, error) {
	file, err := os.Open(path)
	if err != nil {
		return RunToolUse{}, err
	}
	defer file.Close()

	var use RunToolUse
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1024*1024), 64*1024*1024)
	for scanner.Scan() {
		var event struct {
			Type    string `json:"type"`
			Message struct {
				Content []struct {
					Type string `json:"type"`
					Name string `json:"name"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(scanner.Bytes(), &event) != nil || event.Type != "assistant" {
			continue
		}
		use.Turns++
		for _, item := range event.Message.Content {
			if item.Type == "tool_use" {
				use.Calls = append(use.Calls, item.Name)
			}
		}
	}
	return use, scanner.Err()
}

// ToolUseText lists, per run with a transcript, its turns and tool calls: the
// counts by tool, then the sequence with repeats collapsed. This is what
// explains a turn count, which the result totals cannot.
func ToolUseText(results []RunResult) string {
	var b strings.Builder
	for _, result := range results {
		if result.Transcript == "" {
			continue
		}
		use, err := ToolUse(result.Transcript)
		if err != nil {
			continue
		}
		if b.Len() == 0 {
			b.WriteString("\ntool calls per run:\n")
		}
		counts := map[string]int{}
		for _, call := range use.Calls {
			counts[call]++
		}
		verdict := "FAIL"
		if result.Success {
			verdict = "pass"
		}
		fmt.Fprintf(&b, "%s %s/%s %s · %d turns · %d calls · %s\n  %s\n",
			verdict, result.Repository, result.Task, result.Arm, use.Turns, len(use.Calls),
			joinCounts(counts), collapse(use.Calls))
	}
	return b.String()
}

// collapse renders a call sequence with consecutive repeats folded: Read×3.
func collapse(calls []string) string {
	var parts []string
	for i := 0; i < len(calls); {
		j := i
		for j < len(calls) && calls[j] == calls[i] {
			j++
		}
		name := strings.TrimPrefix(calls[i], "mcp__jade__")
		if j-i > 1 {
			parts = append(parts, fmt.Sprintf("%s×%d", name, j-i))
		} else {
			parts = append(parts, name)
		}
		i = j
	}
	return strings.Join(parts, " → ")
}

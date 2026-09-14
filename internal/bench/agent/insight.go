package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
)

// A result's totals say how much a run cost, never why. The why is in the
// transcript: which tools the agent called, in what order, what each returned
// into its context, and where it went round in circles. Analyze reads that
// from Claude Code's stream-json events; InsightReport compares it across arms.

// Tool call categories. Every tool, built-in or Jade, lands in exactly one, so
// the arms can be compared on the work done rather than on tool names.
const (
	CategoryRead   = "read"
	CategorySearch = "search"
	CategoryEdit   = "edit"
	CategoryVerify = "verify"
	CategoryGit    = "git"
	CategoryState  = "state"
	CategoryOther  = "other"
)

var categoryOrder = []string{CategoryRead, CategorySearch, CategoryEdit, CategoryVerify, CategoryGit, CategoryState, CategoryOther}

// ToolCall is one tool call, joined with its result.
type ToolCall struct {
	Index       int    `json:"index"`
	Request     int    `json:"request"`
	Tool        string `json:"tool"`
	Category    string `json:"category"`
	Target      string `json:"target,omitempty"`
	InputBytes  int    `json:"inputBytes"`
	ResultBytes int    `json:"resultBytes"`
	Error       bool   `json:"error,omitempty"`
}

// Insight is what one run's transcript shows.
type Insight struct {
	Repository string  `json:"repository"`
	Task       string  `json:"task"`
	Arm        Arm     `json:"arm"`
	Repeat     int     `json:"repeat"`
	Success    bool    `json:"success"`
	CostUSD    float64 `json:"costUSD"`
	DurationMS int64   `json:"durationMS"`
	Turns      int     `json:"turns"`

	// Tokens is every token the run consumed — the cost measure that holds
	// across models and price changes, unlike dollars — and its parts.
	Tokens           int `json:"tokens"`
	InputTokens      int `json:"inputTokens"`
	CacheWriteTokens int `json:"cacheWriteTokens"`
	CacheReadTokens  int `json:"cacheReadTokens"`

	// JadeStatus is the Jade MCP server's status at session start. A Jade arm
	// whose server did not connect measured nothing about Jade.
	JadeStatus string `json:"jadeStatus,omitempty"`
	// Requests is model round trips; MaxParallel the most tool calls one
	// round trip issued.
	Requests          int `json:"requests"`
	MaxParallel       int `json:"maxParallel"`
	PeakContextTokens int `json:"peakContextTokens"`
	OutputTokens      int `json:"outputTokens"`

	Calls []ToolCall `json:"calls"`
	// CallsBeforeFirstEdit is how much looking preceded the first change;
	// -1 when the run made no edit.
	CallsBeforeFirstEdit int `json:"callsBeforeFirstEdit"`
	Edits                int `json:"edits"`
	VerifyRuns           int `json:"verifyRuns"`
	// RepeatedReads counts reads of a target already read and not edited
	// since: context the agent paid for twice.
	RepeatedReads int `json:"repeatedReads"`
	ToolErrors    int `json:"toolErrors"`
	ResultBytes   int `json:"resultBytes"`
}

// Valid reports whether the run measured its arm: a Jade arm needs Jade.
func (in Insight) Valid() bool {
	return !in.Arm.UsesJade() || in.JadeStatus == "connected"
}

type contentBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

type transcriptEvent struct {
	Type       string `json:"type"`
	Subtype    string `json:"subtype"`
	RequestID  string `json:"request_id"`
	MCPServers []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	} `json:"mcp_servers"`
	Message struct {
		ID      string          `json:"id"`
		Content json.RawMessage `json:"content"`
		Usage   struct {
			Input         int `json:"input_tokens"`
			Output        int `json:"output_tokens"`
			CacheRead     int `json:"cache_read_input_tokens"`
			CacheCreation int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// Analyze reads a run's transcript.
func Analyze(result RunResult) (Insight, error) {
	in := Insight{
		Repository: result.Repository, Task: result.Task, Arm: result.Arm, Repeat: result.Repeat,
		Success: result.Success, CostUSD: result.CostUSD, DurationMS: result.DurationMS, Turns: result.Turns,
		Tokens: result.TotalTokens(), InputTokens: result.InputTokens,
		CacheWriteTokens: result.CacheWriteTokens, CacheReadTokens: result.CacheReadTokens,
		CallsBeforeFirstEdit: -1,
	}
	if result.Transcript == "" {
		return in, fmt.Errorf("%s/%s %s: no transcript", result.Repository, result.Task, result.Arm)
	}
	file, err := os.Open(result.Transcript)
	if err != nil {
		return in, err
	}
	defer file.Close()

	requests := map[string]int{}
	outputByRequest := map[int]int{}
	callsByRequest := map[int]int{}
	callByID := map[string]int{}

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1024*1024), 64*1024*1024)
	for scanner.Scan() {
		var event transcriptEvent
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			continue
		}
		switch event.Type {
		case "system":
			if event.Subtype == "init" {
				for _, server := range event.MCPServers {
					if server.Name == "jade" {
						in.JadeStatus = server.Status
					}
				}
			}
		case "assistant":
			key := event.RequestID
			if key == "" {
				key = event.Message.ID
			}
			request, seen := requests[key]
			if !seen {
				request = len(requests) + 1
				requests[key] = request
			}
			usage := event.Message.Usage
			if context := usage.Input + usage.CacheRead + usage.CacheCreation; context > in.PeakContextTokens {
				in.PeakContextTokens = context
			}
			if usage.Output > outputByRequest[request] {
				outputByRequest[request] = usage.Output
			}
			for _, block := range blocks(event.Message.Content) {
				if block.Type != "tool_use" {
					continue
				}
				call := ToolCall{Index: len(in.Calls) + 1, Request: request, Tool: normalizeTool(block.Name), InputBytes: len(block.Input)}
				call.Category, call.Target = classify(call.Tool, block.Input)
				callByID[block.ID] = len(in.Calls)
				in.Calls = append(in.Calls, call)
				callsByRequest[request]++
			}
		case "user":
			for _, block := range blocks(event.Message.Content) {
				if block.Type != "tool_result" {
					continue
				}
				if index, ok := callByID[block.ToolUseID]; ok {
					in.Calls[index].ResultBytes = resultLength(block.Content)
					in.Calls[index].Error = block.IsError
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return in, err
	}

	in.Requests = len(requests)
	for _, output := range outputByRequest {
		in.OutputTokens += output
	}
	// Streamed assistant events carry the output count at the start of a
	// message, not its final count; the result event has the true total.
	if result.OutputTokens > 0 {
		in.OutputTokens = result.OutputTokens
	}
	for _, count := range callsByRequest {
		if count > in.MaxParallel {
			in.MaxParallel = count
		}
	}

	readSinceEdit := map[string]bool{}
	for i, call := range in.Calls {
		in.ResultBytes += call.ResultBytes
		if call.Error {
			in.ToolErrors++
		}
		switch call.Category {
		case CategoryEdit:
			in.Edits++
			if in.CallsBeforeFirstEdit < 0 {
				in.CallsBeforeFirstEdit = i
			}
			file := targetFile(call.Target)
			for key := range readSinceEdit {
				if file == "" || targetFile(key) == file {
					delete(readSinceEdit, key)
				}
			}
		case CategoryVerify:
			in.VerifyRuns++
		case CategoryRead:
			if call.Target == "" {
				continue
			}
			if readSinceEdit[call.Target] {
				in.RepeatedReads++
			}
			readSinceEdit[call.Target] = true
		}
	}
	return in, nil
}

func blocks(raw json.RawMessage) []contentBlock {
	var list []contentBlock
	if json.Unmarshal(raw, &list) != nil {
		return nil
	}
	return list
}

// resultLength is the text a tool result put into the agent's context.
func resultLength(raw json.RawMessage) int {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return len(text)
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		total := 0
		for _, part := range parts {
			total += len(part.Text)
		}
		return total
	}
	return len(raw)
}

// normalizeTool gives Jade's tools one spelling: mcp__jade__jade_outline is
// jade.outline, the name Jade's own telemetry uses.
func normalizeTool(name string) string {
	if rest, ok := strings.CutPrefix(name, "mcp__jade__"); ok {
		rest = strings.TrimPrefix(strings.TrimPrefix(rest, "jade_"), "jade.")
		return "jade." + rest
	}
	return name
}

var jadeCategories = map[string]string{
	"read_symbol": CategoryRead, "read_range": CategoryRead, "outline": CategoryRead, "context": CategoryRead,
	"find": CategorySearch, "search": CategorySearch, "grep": CategorySearch, "retrieve": CategorySearch,
	"repository_map": CategorySearch, "workspace_tree": CategorySearch, "references": CategorySearch,
	"history": CategorySearch, "search_nudge": CategorySearch,
	"replace_text": CategoryEdit, "replace_range": CategoryEdit, "replace_symbol": CategoryEdit, "insert": CategoryEdit,
	"apply": CategoryEdit, "create_file": CategoryEdit, "replace_file": CategoryEdit, "delete_file": CategoryEdit,
	"delete_symbol": CategoryEdit, "rename": CategoryEdit,
	"check": CategoryVerify, "run_tests": CategoryVerify, "run_command": CategoryVerify,
}

// classify puts a call in a category and names its target: the file, symbol,
// range, pattern or command it was about.
func classify(tool string, input json.RawMessage) (string, string) {
	var args map[string]interface{}
	_ = json.Unmarshal(input, &args)
	text := func(key string) string {
		value, _ := args[key].(string)
		return value
	}
	number := func(key string) string {
		if value, ok := args[key].(float64); ok {
			return fmt.Sprintf("%d", int(value))
		}
		return ""
	}

	switch tool {
	case "Read":
		target := workspaceRelative(text("file_path"))
		if offset := number("offset"); offset != "" {
			target += ":" + offset + "+" + number("limit")
		}
		return CategoryRead, target
	case "Grep", "Glob", "LS":
		return CategorySearch, text("pattern")
	case "Edit", "MultiEdit", "Write", "NotebookEdit":
		return CategoryEdit, workspaceRelative(text("file_path"))
	case "Bash":
		command := text("command")
		return classifyCommand(command), clip(command, 160)
	}

	name, ok := strings.CutPrefix(tool, "jade.")
	if !ok {
		return CategoryOther, ""
	}
	target := text("path")
	switch name {
	case "read_symbol", "context":
		if symbol := text("symbolId") + text("symbolName"); symbol != "" {
			target += "#" + symbol
		}
	case "read_range":
		target += ":" + number("startLine") + "-" + number("endLine")
	case "find", "search", "grep", "retrieve", "repository_map":
		target = text("query")
	case "replace_symbol":
		target = text("symbolId")
	case "apply":
		target = ""
	case "check":
		target = text("kind")
	case "run_command":
		target = text("name")
	}
	category, known := jadeCategories[name]
	if !known {
		category = CategoryState
	}
	return category, target
}

var verifyCommands = []string{
	"go test", "go build", "go vet", "cargo test", "cargo build", "cargo check", "cargo clippy",
	"npm test", "npm run test", "npm run build", "npx ava", "npx tsc", "tsc ", "npx xo",
	"pytest", "python -m pytest", "python3 -m pytest", "make test", "make build", "make check",
}

// classifyCommand reads what a shell command was for. Verification wins over
// everything else in a compound command, since running the tests is the step
// that decides what the agent does next.
func classifyCommand(command string) string {
	lower := strings.ToLower(strings.TrimSpace(command))
	for _, marker := range verifyCommands {
		if strings.Contains(lower, marker) {
			return CategoryVerify
		}
	}
	redirects := strings.NewReplacer("2>&1", "", "2>/dev/null", "", ">/dev/null", "", "> /dev/null", "").Replace(lower)
	if strings.Contains(redirects, "sed -i") || strings.Contains(redirects, ">") || strings.Contains(redirects, "tee ") ||
		strings.Contains(redirects, "git apply") || strings.Contains(redirects, "patch ") {
		return CategoryEdit
	}
	first := strings.Fields(strings.NewReplacer("&&", " ", ";", " ", "|", " ", "(", " ").Replace(lower))
	if len(first) == 0 {
		return CategoryOther
	}
	switch first[0] {
	case "git":
		return CategoryGit
	case "cat", "head", "tail", "nl", "less", "wc", "sed", "awk", "bat":
		return CategoryRead
	case "grep", "rg", "find", "ls", "tree", "fd", "ag", "egrep":
		return CategorySearch
	case "cd":
		if len(first) > 2 {
			return classifyCommand(strings.Join(first[2:], " "))
		}
	}
	return CategoryOther
}

func workspaceRelative(path string) string {
	if index := strings.Index(path, "/workspace/"); index >= 0 {
		return path[index+len("/workspace/"):]
	}
	return path
}

// targetFile is the file part of a target: main.go#Run and main.go:10-20 are
// both main.go.
func targetFile(target string) string {
	if index := strings.IndexAny(target, "#:"); index >= 0 {
		return target[:index]
	}
	return target
}

func clip(text string, max int) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) <= max {
		return text
	}
	return text[:max] + "…"
}

// Insights analyses every run with a transcript. Runs that cannot be read
// are counted, not silently dropped.
func Insights(results []RunResult) ([]Insight, int) {
	var insights []Insight
	unreadable := 0
	for _, result := range results {
		insight, err := Analyze(result)
		if err != nil {
			unreadable++
			continue
		}
		insights = append(insights, insight)
	}
	return insights, unreadable
}

type group struct {
	insights []Insight
}

func (g group) mean(value func(Insight) float64) float64 {
	if len(g.insights) == 0 {
		return 0
	}
	total := 0.0
	for _, in := range g.insights {
		total += value(in)
	}
	return total / float64(len(g.insights))
}

func (g group) spread(value func(Insight) float64) string {
	low, high := math.Inf(1), math.Inf(-1)
	for _, in := range g.insights {
		low, high = math.Min(low, value(in)), math.Max(high, value(in))
	}
	if len(g.insights) < 2 || low == high {
		return ""
	}
	return fmt.Sprintf("(%.0f–%.0f)", low, high)
}

func (g group) categoryCalls(category string) float64 {
	return g.mean(func(in Insight) float64 {
		count := 0
		for _, call := range in.Calls {
			if call.Category == category {
				count++
			}
		}
		return float64(count)
	})
}

// InsightReport compares arms task by task, then overall, then looks inside
// Jade's own tools and at the outliers.
func InsightReport(results []RunResult) string {
	insights, unreadable := Insights(results)
	if len(insights) == 0 {
		return ""
	}
	var b strings.Builder

	var invalid []Insight
	var valid []Insight
	for _, in := range insights {
		if in.Valid() {
			valid = append(valid, in)
		} else {
			invalid = append(invalid, in)
		}
	}
	fmt.Fprintf(&b, "\n== insight: %d runs analysed", len(valid))
	if len(invalid) > 0 {
		fmt.Fprintf(&b, ", %d invalid (Jade not connected)", len(invalid))
	}
	if unreadable > 0 {
		fmt.Fprintf(&b, ", %d without a readable transcript", unreadable)
	}
	b.WriteString("\n")

	header := "  %-11s %2s %5s %11s %6s %6s %6s %6s %6s %7s %6s %7s %5s %6s %5s %6s %5s\n"
	row := "  %-11s %2d %4.0f%% %4.1f%-7s %6.1f %6.1f %6.1f %6.1f %6.1f %7.1f %6.1f %7.1f %5.1f %6.0f %5.0f %6.0f %5.0f\n"
	columns := func() {
		fmt.Fprintf(&b, header, "arm", "n", "pass", "turns", "calls", "read", "search", "edit", "verify",
			">edit", "reread", "errors", "par", "ctx k", "res k", "tok k", "sec")
	}
	line := func(arm Arm, g group) {
		pass := g.mean(func(in Insight) float64 { return boolFloat(in.Success) }) * 100
		turns := g.mean(func(in Insight) float64 { return float64(in.Turns) })
		fmt.Fprintf(&b, row, arm, len(g.insights), pass, turns, g.spread(func(in Insight) float64 { return float64(in.Turns) }),
			g.mean(func(in Insight) float64 { return float64(len(in.Calls)) }),
			g.categoryCalls(CategoryRead), g.categoryCalls(CategorySearch), g.categoryCalls(CategoryEdit), g.categoryCalls(CategoryVerify),
			g.mean(func(in Insight) float64 { return float64(in.CallsBeforeFirstEdit) }),
			g.mean(func(in Insight) float64 { return float64(in.RepeatedReads) }),
			g.mean(func(in Insight) float64 { return float64(in.ToolErrors) }),
			g.mean(func(in Insight) float64 { return float64(in.MaxParallel) }),
			g.mean(func(in Insight) float64 { return float64(in.PeakContextTokens) / 1000 }),
			g.mean(func(in Insight) float64 { return float64(in.ResultBytes) / 1000 }),
			g.mean(func(in Insight) float64 { return float64(in.Tokens) / 1000 }),
			g.mean(func(in Insight) float64 { return float64(in.DurationMS) / 1000 }))
	}
	byArm := func(list []Insight) map[Arm]group {
		groups := map[Arm]group{}
		for _, in := range list {
			g := groups[in.Arm]
			g.insights = append(g.insights, in)
			groups[in.Arm] = g
		}
		return groups
	}

	b.WriteString("\nper task (means over repeats; turns range in brackets; >edit = calls before the first edit; par = max parallel calls; ctx = peak context; res = tool result KB; tok = total tokens, input + cache write + cache read + output)\n")
	var tasks []string
	seen := map[string]bool{}
	for _, in := range valid {
		key := in.Repository + "/" + in.Task
		if !seen[key] {
			seen[key] = true
			tasks = append(tasks, key)
		}
	}
	for _, task := range tasks {
		var list []Insight
		for _, in := range valid {
			if in.Repository+"/"+in.Task == task {
				list = append(list, in)
			}
		}
		fmt.Fprintf(&b, "\n%s\n", task)
		columns()
		groups := byArm(list)
		for _, arm := range []Arm{ArmShell, ArmShellLean, ArmJade, ArmJadeShell} {
			if g, ok := groups[arm]; ok {
				line(arm, g)
			}
		}
	}

	b.WriteString("\nall tasks\n")
	columns()
	groups := byArm(valid)
	for _, arm := range []Arm{ArmShell, ArmShellLean, ArmJade, ArmJadeShell} {
		if g, ok := groups[arm]; ok {
			line(arm, g)
		}
	}

	b.WriteString("\ntokens per run by kind (means, k)\n")
	for _, arm := range []Arm{ArmShell, ArmShellLean, ArmJade, ArmJadeShell} {
		g, ok := groups[arm]
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "  %-11s total %6.0f · cache read %6.0f · cache write %5.0f · input %4.1f · output %5.1f\n", arm,
			g.mean(func(in Insight) float64 { return float64(in.Tokens) / 1000 }),
			g.mean(func(in Insight) float64 { return float64(in.CacheReadTokens) / 1000 }),
			g.mean(func(in Insight) float64 { return float64(in.CacheWriteTokens) / 1000 }),
			g.mean(func(in Insight) float64 { return float64(in.InputTokens) / 1000 }),
			g.mean(func(in Insight) float64 { return float64(in.OutputTokens) / 1000 }))
	}

	b.WriteString("\ntool result bytes by category (share of each arm's context from tools)\n")
	for _, arm := range []Arm{ArmShell, ArmJade, ArmJadeShell} {
		g, ok := groups[arm]
		if !ok {
			continue
		}
		totals := map[string]int{}
		all := 0
		for _, in := range g.insights {
			for _, call := range in.Calls {
				totals[call.Category] += call.ResultBytes
				all += call.ResultBytes
			}
		}
		var parts []string
		for _, category := range categoryOrder {
			if totals[category] > 0 && all > 0 {
				parts = append(parts, fmt.Sprintf("%s %.0f%%", category, 100*float64(totals[category])/float64(all)))
			}
		}
		fmt.Fprintf(&b, "  %-11s %s\n", arm, strings.Join(parts, " · "))
	}

	type toolStats struct {
		calls, errors, bytes int
		runs                 map[string]bool
	}
	stats := map[string]*toolStats{}
	for _, in := range valid {
		if in.Arm == ArmShell {
			continue
		}
		for _, call := range in.Calls {
			s := stats[call.Tool]
			if s == nil {
				s = &toolStats{runs: map[string]bool{}}
				stats[call.Tool] = s
			}
			s.calls++
			s.bytes += call.ResultBytes
			s.runs[fmt.Sprintf("%s/%s/%s/%d", in.Repository, in.Task, in.Arm, in.Repeat)] = true
			if call.Error {
				s.errors++
			}
		}
	}
	if len(stats) > 0 {
		b.WriteString("\ntools used by the Jade arms (calls · errors · mean result bytes · runs using it)\n")
		names := make([]string, 0, len(stats))
		for name := range stats {
			names = append(names, name)
		}
		sort.Slice(names, func(i, j int) bool {
			if stats[names[i]].calls != stats[names[j]].calls {
				return stats[names[i]].calls > stats[names[j]].calls
			}
			return names[i] < names[j]
		})
		for _, name := range names {
			s := stats[name]
			fmt.Fprintf(&b, "  %-22s %4d · %3d · %6d · %d\n", name, s.calls, s.errors, s.bytes/s.calls, len(s.runs))
		}
	}

	type largest struct {
		run  string
		call ToolCall
	}
	var big []largest
	for _, in := range valid {
		for _, call := range in.Calls {
			big = append(big, largest{fmt.Sprintf("%s/%s %s #%d", in.Repository, in.Task, in.Arm, in.Repeat), call})
		}
	}
	sort.Slice(big, func(i, j int) bool { return big[i].call.ResultBytes > big[j].call.ResultBytes })
	if len(big) > 8 {
		big = big[:8]
	}
	if len(big) > 0 {
		b.WriteString("\nlargest tool results\n")
		for _, item := range big {
			fmt.Fprintf(&b, "  %6d B  %-18s %s · %s\n", item.call.ResultBytes, item.call.Tool, clip(item.call.Target, 70), item.run)
		}
	}

	for _, in := range invalid {
		fmt.Fprintf(&b, "\ninvalid: %s/%s %s #%d — jade status %q\n", in.Repository, in.Task, in.Arm, in.Repeat, in.JadeStatus)
	}
	return b.String()
}

func boolFloat(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

// WriteInsights writes one JSON line per analysed run, calls included, for
// analysis beyond the report.
func WriteInsights(path string, results []RunResult) error {
	insights, _ := Insights(results)
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	for _, in := range insights {
		if err := encoder.Encode(in); err != nil {
			return err
		}
	}
	return nil
}

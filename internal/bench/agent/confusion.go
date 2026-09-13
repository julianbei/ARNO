package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/julianbei/jade/internal/telemetry"
)

// ConfusionText reports, across every Jade-arm run that kept telemetry, how
// often each Jade tool was called, which inspect tools were switched on the
// same target, and which tools were retried after a failed answer. Release
// plan Phase 3 merges and cuts tools from this.
//
// Sequences are analysed per run and only then summed: one run's last call
// and the next run's first are unrelated.
func ConfusionText(results []RunResult) string {
	calls := map[string]int{}
	switches := map[string]int{}
	retries := map[string]int{}
	runs := 0

	for _, result := range results {
		if result.TelemetryDir == "" {
			continue
		}
		records := readTelemetry(result.TelemetryDir)
		if len(records) == 0 {
			continue
		}
		runs++
		for _, record := range records {
			calls[record.Tool]++
		}
		report := telemetry.AnalyzeConfusion(records, nil)
		for _, change := range report.Switches {
			switches[change.From+"→"+change.To] += change.Count
		}
		for _, retry := range report.Retries {
			retries[fmt.Sprintf("%s after %s", retry.Tool, retry.After)] += retry.Count
		}
	}
	if runs == 0 {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\ntool confusion across %d Jade-arm runs:\n", runs)
	fmt.Fprintf(&b, "jade tool calls: %s\n", joinCounts(calls))
	if len(switches) > 0 {
		fmt.Fprintf(&b, "switched tools on the same target: %s\n", joinCounts(switches))
	}
	if len(retries) > 0 {
		fmt.Fprintf(&b, "retried after a failed answer: %s\n", joinCounts(retries))
	}
	return b.String()
}

// readTelemetry reads every telemetry log under dir. A missing or unreadable
// log yields nothing: a run whose agent never called Jade has no log at all.
func readTelemetry(dir string) []telemetry.Record {
	var records []telemetry.Record
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			var record telemetry.Record
			if json.Unmarshal(scanner.Bytes(), &record) == nil && record.Tool != "" {
				records = append(records, record)
			}
		}
		// A truncated read keeps what was read: a partial log still says more
		// than none.
		_ = scanner.Err()
		return nil
	})
	return records
}

// joinCounts renders counts largest first, ties by name.
func joinCounts(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(a, b int) bool {
		if counts[keys[a]] != counts[keys[b]] {
			return counts[keys[a]] > counts[keys[b]]
		}
		return keys[a] < keys[b]
	})
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s %d", key, counts[key]))
	}
	return strings.Join(parts, ", ")
}

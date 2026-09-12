// Package render turns jade's typed responses into plain text for the MCP
// transport.
//
// The consumer of every jade response is a language model, not a parser. MCP
// already delivers tool results as text content, so JSON is a serialization
// tax paid on every call: braces, quotes, repeated field names, indentation,
// and nulls for fields nobody populated. The 8.3 benchmark measured that tax
// at 5.63x shell's token cost across seven realistic questions, with roughly
// every response landing at 1,100-1,600 tokens regardless of how small the
// actual answer was.
//
// Rendering happens at the transport boundary, so the internal structs stay
// typed and testable and only the wire format changes.
//
// House style, applied by every renderer here:
//   - the decisive content comes first, on the first line where possible;
//   - empty, null and zero-valued fields are omitted entirely rather than
//     serialized as evidence of their own absence;
//   - source code is emitted as source code, not as a quoted string with
//     escaped newlines;
//   - structure appears only where a caller must branch on it.
package render

import (
	"fmt"
	"sort"
	"strings"

	"github.com/julianbei/jade/internal/protocol"
)

// Text renders a known response type. The bool reports whether a renderer
// existed: false means the caller should fall back to JSON rather than
// inventing a format, so an unrendered type degrades to the old behavior
// instead of losing information.
func Text(value interface{}) (string, bool) {
	switch v := value.(type) {
	case protocol.InspectResponse:
		return inspect(v), true
	case protocol.SearchResponse:
		return search(v), true
	case protocol.FindResponse:
		return find(v), true
	case protocol.ReferencesResponse:
		return references(v), true
	case protocol.ChangesResponse:
		return changes(v), true
	case protocol.WorkspaceTreeResponse:
		return workspaceTree(v), true
	case protocol.DiffResponse:
		return diff(v), true
	case protocol.EditResponse:
		return edit(v), true
	case protocol.JobStatusResponse:
		return jobStatus(v), true
	case protocol.JobOutputResponse:
		return jobOutput(v), true
	case protocol.RunTestsResponse:
		return runTests(v), true
	case protocol.SearchNudgeResponse:
		return searchNudge(v), true
	case protocol.RepositoryMapResponse:
		return repositoryMap(v), true
	case protocol.RetrievalResponse:
		return retrieval(v), true
	case protocol.EventsResponse:
		return events(v), true
	case protocol.CheckpointResponse:
		return checkpoint(v), true
	case protocol.ContextResponse:
		return contextResponse(v), true
	case protocol.HistoryResponse:
		return history(v), true
	case protocol.CheckResponse:
		return check(v), true
	case protocol.ApplyResponse:
		return apply(v), true
	case protocol.TelemetryResponse:
		return telemetryResponse(v), true
	case protocol.GrepResponse:
		return grep(v), true
	case protocol.RunCommandResponse:
		return runCommand(v), true
	case protocol.DeclareCommandResponse:
		return declareCommand(v), true
	default:
		return "", false
	}
}

// ambiguous renders the candidate list a caller has to choose from.
//
// One line each, leading with the ID they will pass back and ending with the
// declaration itself. Bare IDs used to be all they got, which for two methods
// named Put in one file meant choosing between `store.go::Put@48` and
// `store.go::Put@72` — no information at all, so the caller spent a turn
// fetching one to find out. The receiver is the whole distinction and it costs
// one line to show.
func ambiguous(resolve protocol.SymbolResolution) string {
	if len(resolve.Candidates) == 0 {
		// Older callers and transports that only populate CandidateIDs.
		return fmt.Sprintf("ambiguous: %s matches %s",
			resolve.Query, strings.Join(resolve.CandidateIDs, ", "))
	}

	lines := make([]string, 0, len(resolve.Candidates)+1)
	lines = append(lines, fmt.Sprintf("ambiguous: %s matches %d declarations", resolve.Query, len(resolve.Candidates)))
	for _, candidate := range resolve.Candidates {
		line := "  " + candidate.ID
		if candidate.Signature != "" {
			line += "  " + candidate.Signature
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// notFound renders a missing symbol, naming the same-named declarations that
// do exist when there are any. The usual cause is a caller who knows the name
// and guessed the ID — an omitted "@line" suffix, or one that has gone stale
// since they last looked — and that is recoverable in the response rather
// than in another round trip.
//
// With no candidates it stays a bare "not found": inventing a near-miss for a
// name that is genuinely absent would be a confident wrong answer.
func notFound(resolve protocol.SymbolResolution) string {
	if len(resolve.CandidateIDs) == 0 {
		return fmt.Sprintf("not found: %s", resolve.Query)
	}
	if len(resolve.CandidateIDs) == 1 {
		return fmt.Sprintf("not found: %s — did you mean %s?", resolve.Query, resolve.CandidateIDs[0])
	}
	return fmt.Sprintf("not found: %s — did you mean one of: %s?",
		resolve.Query, strings.Join(resolve.CandidateIDs, ", "))
}

// parserNote renders the incompleteness warning, and nothing at all when the
// outline came from a real grammar. Silence is correct in the common case:
// a caveat printed on every Go file would be ignored by the time it mattered.
func parserNote(parser protocol.ParserInfo) string {
	if parser.Complete || parser.Note == "" {
		return ""
	}
	return "! " + parser.Note
}

func inspect(r protocol.InspectResponse) string {
	lines := make([]string, 0, 8)

	// Resolution first when it is not a clean hit: an ambiguous or missing
	// symbol is the whole answer, and burying it under an outline wastes the
	// reader's first line.
	switch r.Resolve.Status {
	case protocol.ResolutionAmbiguous:
		return ambiguous(r.Resolve)
	case protocol.ResolutionNotFound:
		return notFound(r.Resolve)
	}

	header := make([]string, 0, 3)
	if r.Resolve.SelectedID != "" {
		header = append(header, r.Resolve.SelectedID)
	}
	if r.Revision != "" {
		header = append(header, r.Revision)
	}
	if freshness := freshnessLine(r.Freshness); freshness != "" {
		header = append(header, freshness)
	}
	if len(header) > 0 {
		lines = append(lines, strings.Join(header, " · "))
	}

	// The parser caveat goes *above* the outline, not below it. A reader who
	// stops after the declarations they were looking for must still have seen
	// that the list may be incomplete — the whole failure mode is trusting an
	// absence, and a footnote after the data is read too late to prevent it.
	if note := parserNote(r.Parser); note != "" {
		lines = append(lines, note)
	}

	if outline := outlineLines(r.Outline); len(outline) > 0 {
		lines = append(lines, outline...)
	}

	if r.Source != "" {
		// Source as source: the single largest saving in the whole renderer,
		// since JSON escaping inflates every newline and tab in a file.
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, r.Source)
	}

	if len(lines) == 0 {
		return "(empty)"
	}
	return strings.Join(lines, "\n")
}

func outlineLines(items []protocol.OutlineItem) []string {
	lines := make([]string, 0, len(items))
	for _, item := range items {
		lines = append(lines, fmt.Sprintf("%d-%d %s %s", item.From, item.To, item.Kind, item.Name))
	}
	return lines
}

// freshnessLine reduces Freshness to its decisive bit. The full changed and
// dirty path lists — which were byte-for-byte identical 35-element arrays on
// every read in this repo — belong in changes(), which exists to report
// exactly that.
func freshnessLine(f protocol.Freshness) string {
	if f.Unknown != "" {
		return "freshness unknown: " + f.Unknown
	}
	if !f.Drifted {
		return ""
	}
	count := len(f.ChangedPaths)
	if count == 0 {
		count = len(f.DirtyPaths)
	}
	return fmt.Sprintf("drifted: %d files", count)
}

// find renders each match as a grep-style location header followed by the
// declaration itself — deliberately close to what `grep -A` prints, since
// that is the output shape this replaces and the one callers already read
// fluently.
func find(r protocol.FindResponse) string {
	if len(r.Results) == 0 {
		if r.Summary != "" {
			return r.Summary
		}
		return "no matches"
	}

	lines := make([]string, 0, len(r.Results)*4)
	if r.Total > len(r.Results) {
		lines = append(lines, r.Summary)
	}
	for index, result := range r.Results {
		if index > 0 {
			lines = append(lines, "")
		}
		header := fmt.Sprintf("%s:%d-%d %s %s", result.Path, result.StartLine, result.EndLine, result.Kind, result.Symbol)
		if result.Truncated {
			header += " (truncated)"
		}
		lines = append(lines, header, result.Body)
	}
	return strings.Join(lines, "\n")
}

func search(r protocol.SearchResponse) string {
	if len(r.Hits) == 0 {
		return fmt.Sprintf("no hits for %q", r.Query)
	}

	lines := make([]string, 0, len(r.Hits))
	for _, hit := range r.Hits {
		location := hit.Path
		if hit.StartLine > 0 {
			location = fmt.Sprintf("%s:%d", hit.Path, hit.StartLine)
		}
		line := location
		if hit.Symbol != "" {
			line += " " + hit.Symbol
		}
		if hit.Kind != "" {
			line += " (" + hit.Kind + ")"
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func references(r protocol.ReferencesResponse) string {
	if len(r.References) == 0 {
		return fmt.Sprintf("no references to %s (%s)", r.Query, r.Source)
	}

	lines := make([]string, 0, len(r.References)+1)
	for _, ref := range r.References {
		line := ref.Path
		if ref.Line > 0 {
			line = fmt.Sprintf("%s:%d:%d", ref.Path, ref.Line, ref.Column)
		}
		// Without a line number the symbol name is the only thing telling
		// two references in one file apart, so it earns its space.
		if ref.Symbol != "" {
			line += " " + ref.Symbol
		}
		if ref.Confidence != "" {
			line += " (" + ref.Confidence + ")"
		}
		lines = append(lines, line)
	}
	if r.Summary != "" {
		lines = append(lines, r.Summary)
	}
	return strings.Join(lines, "\n")
}

// maxRenderedSymbolChanges bounds the symbol-level detail in a changes()
// response. A wide refactor can touch hundreds of symbols, and printing all
// of them would undo Phase 10's savings on the one response an agent calls
// most often. The count always reports the true total, so the cap costs
// detail, never accuracy.
const maxRenderedSymbolChanges = 40

// maxRenderedChangedFiles bounds the file list. Symbols were capped from the
// start but the file list never was, so a branch with 88 changed files printed
// all 88 — the single largest contributor to changes() being the most
// expensive call jade makes.
const maxRenderedChangedFiles = 40

func changes(r protocol.ChangesResponse) string {
	// The revision leads, and is printed even for a clean tree. It is a
	// required argument of every edit tool, and changes() is where a caller
	// goes to find it — dropping it as "structure" made edits impossible to
	// address without guessing. Found live, the first time a stale-revision
	// rejection left nowhere to look up the current value.
	head := r.Revision
	if len(r.Files) == 0 {
		if head == "" {
			return "no changes"
		}
		return head + " · no changes"
	}

	lines := make([]string, 0, len(r.Files)+len(r.Symbols)+2)
	summary := r.Summary
	if head != "" {
		if summary == "" {
			summary = head
		} else {
			summary = head + " · " + summary
		}
	}
	if summary != "" {
		lines = append(lines, summary)
	}

	symbolsByPath := make(map[string][]protocol.SymbolChange, len(r.Symbols))
	for _, change := range r.Symbols {
		symbolsByPath[change.Path] = append(symbolsByPath[change.Path], change)
	}

	if r.SymbolsOmitted > 0 {
		// Said explicitly, because "no symbol lines" otherwise reads as "no
		// symbols changed" — a much stronger and quite wrong claim.
		lines = append(lines, fmt.Sprintf("symbol changes omitted (%d files changed) · use outline on one file", r.SymbolsOmitted))
	}

	printed := 0
	files := r.Files
	omittedFiles := 0
	if len(files) > maxRenderedChangedFiles {
		omittedFiles = len(files) - maxRenderedChangedFiles
		files = files[:maxRenderedChangedFiles]
	}

	for _, file := range files {
		lines = append(lines, fmt.Sprintf("+%d -%d %s", file.Added, file.Removed, file.Path))
		// Symbols are nested under their file rather than repeating the path
		// on every line — the file is already named directly above.
		for _, change := range symbolsByPath[file.Path] {
			if printed >= maxRenderedSymbolChanges {
				break
			}
			lines = append(lines, fmt.Sprintf("  %s %s %s", change.Change, change.Kind, change.Symbol))
			printed++
		}
	}

	if omittedFiles > 0 {
		lines = append(lines, fmt.Sprintf("... %d more files · use diff for the full patch", omittedFiles))
	}
	if remaining := len(r.Symbols) - printed; remaining > 0 {
		lines = append(lines, fmt.Sprintf("... %d more symbol changes", remaining))
	}

	return strings.Join(lines, "\n")
}

func workspaceTree(r protocol.WorkspaceTreeResponse) string {
	lines := make([]string, 0, len(r.Entries)+2)
	for _, entry := range r.Entries {
		if entry.IsDir {
			lines = append(lines, entry.Path+"/")
			continue
		}
		lines = append(lines, entry.Path)
	}
	if r.Truncated {
		lines = append(lines, fmt.Sprintf("... truncated at %d entries", len(r.Entries)))
	}
	if len(lines) == 0 {
		return "(empty tree)"
	}
	return strings.Join(lines, "\n")
}

func diff(r protocol.DiffResponse) string {
	summary := r.Summary
	if summary == "" {
		summary = "no changes"
	}
	if strings.TrimSpace(r.Patch) == "" {
		return summary
	}
	return summary + "\n\n" + r.Patch
}

func edit(r protocol.EditResponse) string {
	lines := make([]string, 0, 6)

	head := fmt.Sprintf("%s → %s", r.OldRevision, r.NewRevision)
	if len(r.Changed) > 0 {
		head += " · " + strings.Join(r.Changed, ", ")
	}
	if r.AddedLines > 0 || r.RemovedLines > 0 {
		head += fmt.Sprintf(" · +%d -%d", r.AddedLines, r.RemovedLines)
	}
	// Only mentioned when the formatter actually rewrote something, so the
	// common case (already well-formed) costs nothing to report.
	if len(r.Formatted) > 0 {
		head += fmt.Sprintf(" · formatted %d", len(r.Formatted))
	}
	lines = append(lines, head)

	// Diagnostics are why an agent reads an edit response at all, so they
	// get their own lines rather than being folded into the header.
	for _, d := range r.Diagnostics {
		lines = append(lines, fmt.Sprintf("%s %s:%d:%d %s", d.Level, d.Path, d.Line, d.Column, d.Message))
	}
	if len(r.Jobs) > 0 {
		lines = append(lines, "jobs: "+strings.Join(r.Jobs, ", "))
	}
	return strings.Join(lines, "\n")
}

func jobStatus(r protocol.JobStatusResponse) string {
	line := strings.TrimSpace(fmt.Sprintf("%s %s %s", r.ID, r.Kind, r.Status))
	if line == "" {
		// Never render to nothing: an empty response leaves the agent unable
		// to tell a result from a dropped one.
		line = "(no job)"
	}
	if r.Summary != "" {
		return line + "\n" + r.Summary
	}
	return line
}

func jobOutput(r protocol.JobOutputResponse) string {
	head := strings.TrimSpace(fmt.Sprintf("%s %s %s", r.ID, r.Kind, r.Status))
	if head == "" {
		head = "(no job)"
	}
	if r.OmittedBytes > 0 {
		head += fmt.Sprintf(" (%d bytes omitted)", r.OmittedBytes)
	}

	lines := []string{head}
	if r.RawOutput != "" {
		lines = append(lines, r.RawOutput)
	} else if r.Summary != "" {
		lines = append(lines, r.Summary)
	}
	return strings.Join(lines, "\n")
}

func searchNudge(r protocol.SearchNudgeResponse) string {
	if !r.Nudged {
		return "no nudge"
	}
	return r.Footer
}

func repositoryMap(r protocol.RepositoryMapResponse) string {
	lines := make([]string, 0, len(r.Included)+2)
	lines = append(lines, fmt.Sprintf("%d/%d tokens · %d included, %d omitted",
		r.UsedTokens, r.MaxTokens, len(r.Included), len(r.Omitted)))
	for _, item := range r.Included {
		lines = append(lines, fmt.Sprintf("%s (%d symbols, ~%d tokens)",
			item.Path, item.SymbolCount, item.EstimatedCost))
	}
	return strings.Join(lines, "\n")
}
func retrieval(r protocol.RetrievalResponse) string {
	lines := make([]string, 0, len(r.Candidates)+2)
	head := fmt.Sprintf("%d/%d tokens", r.UsedTokens, r.MaxTokens)
	if r.BudgetExceeded {
		head += " · BUDGET EXCEEDED"
	}
	lines = append(lines, head)
	for _, candidate := range r.Candidates {
		line := candidate.Path
		if candidate.StartLine > 0 {
			line = fmt.Sprintf("%s:%d-%d", candidate.Path, candidate.StartLine, candidate.EndLine)
		}
		if candidate.Symbol != "" {
			line += " " + candidate.Symbol
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func events(r protocol.EventsResponse) string {
	if len(r.Events) == 0 {
		return fmt.Sprintf("no events (cursor %d)", r.Cursor)
	}

	lines := make([]string, 0, len(r.Events))
	for _, record := range r.Events {
		line := fmt.Sprintf("%d %s %s", record.Cursor, record.Event.Type, record.Event.Entity)
		// Payload keys are sorted so the same event never renders two
		// different ways between calls — map iteration order otherwise makes
		// output unstable and diffs unreadable.
		if len(record.Event.Payload) > 0 {
			keys := make([]string, 0, len(record.Event.Payload))
			for key := range record.Event.Payload {
				keys = append(keys, key)
			}
			sort.Strings(keys)

			pairs := make([]string, 0, len(keys))
			for _, key := range keys {
				pairs = append(pairs, key+"="+record.Event.Payload[key])
			}
			line += " " + strings.Join(pairs, " ")
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func checkpoint(r protocol.CheckpointResponse) string {
	line := fmt.Sprintf("%s at %s", r.ID, r.Revision)
	if r.Note != "" {
		line += " · " + r.Note
	}
	if len(r.Paths) > 0 {
		line += fmt.Sprintf(" · %d files", len(r.Paths))
	}
	return line
}

func contextResponse(r protocol.ContextResponse) string {
	head := r.Summary
	if head == "" {
		head = strings.TrimSpace(r.Symbol + " " + r.Kind)
	}
	if head == "" {
		head = "(no context)"
	}
	lines := []string{head}

	// Diagnostics before the code: if the symbol is currently broken, that
	// changes what the agent does with everything below it.
	for _, d := range r.Diagnostics {
		lines = append(lines, fmt.Sprintf("%s %s:%d:%d %s", d.Level, d.Path, d.Line, d.Column, d.Message))
	}

	lines = append(lines, section("callers", r.Callers, r.CallerCount)...)
	lines = append(lines, section("tests", r.Tests, r.TestCount)...)
	lines = append(lines, section("types", r.Types, r.TypeCount)...)

	if r.Implementation != "" {
		lines = append(lines, "", r.Implementation)
	}
	return strings.Join(lines, "\n")
}

// section renders one capped list, naming the true total whenever the cap
// bit so the reader knows detail was dropped rather than absent.
func section(name string, values []string, total int) []string {
	if len(values) == 0 {
		return nil
	}
	header := fmt.Sprintf("%s (%d):", name, total)
	if total > len(values) {
		header = fmt.Sprintf("%s (%d, showing %d):", name, total, len(values))
	}
	return append([]string{header}, prefixAll(values, "  ")...)
}

func prefixAll(values []string, prefix string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, prefix+value)
	}
	return out
}

func history(r protocol.HistoryResponse) string {
	if len(r.Commits) == 0 {
		if r.Summary != "" {
			return r.Summary
		}
		return "no history"
	}

	lines := make([]string, 0, len(r.Commits)+3)
	lines = append(lines, r.Summary)
	for _, commit := range r.Commits {
		lines = append(lines, fmt.Sprintf("%s %s %s — %s",
			commit.SHA, commit.Date, commit.Author, commit.Subject))
	}
	if r.Patch != "" {
		lines = append(lines, "", r.Patch)
	}
	if r.OmittedBytes > 0 {
		lines = append(lines, fmt.Sprintf("(%d bytes of patch omitted)", r.OmittedBytes))
	}
	return strings.Join(lines, "\n")
}

func apply(r protocol.ApplyResponse) string {
	head := fmt.Sprintf("%s → %s", r.OldRevision, r.NewRevision)
	if r.Summary != "" {
		head += " · " + r.Summary
	}
	lines := []string{strings.TrimSpace(head)}
	if lines[0] == "→" {
		lines[0] = "(no edits)"
	}

	// Diagnostics before the file list: a batch that compiled is routine, a
	// batch that broke something is the thing the caller must act on.
	for _, d := range r.Diagnostics {
		lines = append(lines, fmt.Sprintf("%s %s:%d:%d %s", d.Level, d.Path, d.Line, d.Column, d.Message))
	}
	if r.CheckSummary != "" {
		lines = append(lines, r.CheckSummary)
	}
	for _, path := range r.Changed {
		lines = append(lines, "  "+path)
	}
	return strings.Join(lines, "\n")
}

// telemetryResponse leads with the fallback counts rather than the usage
// table. Which tools are popular is mildly interesting; which ones failed in a
// way that sends the caller to bash is the number the log exists to surface.
func telemetryResponse(r protocol.TelemetryResponse) string {
	if r.Cleared {
		return "telemetry cleared · " + r.Path
	}
	if r.TotalCalls == 0 {
		if r.Summary != "" {
			return r.Summary
		}
		return "no tool calls recorded yet"
	}

	lines := make([]string, 0, len(r.Tools)+len(r.Fallbacks)+3)
	lines = append(lines, r.Summary)

	if len(r.Fallbacks) > 0 {
		parts := make([]string, 0, len(r.Fallbacks))
		for _, failure := range r.Fallbacks {
			parts = append(parts, fmt.Sprintf("%s %d", failure.Outcome, failure.Count))
		}
		lines = append(lines, "fallback risk: "+strings.Join(parts, ", "))
	}

	for _, stats := range r.Tools {
		line := fmt.Sprintf("%s  %d calls  %s  avg %dms", stats.Tool, stats.Calls, humanBytes(stats.Bytes), stats.AvgMS)
		if stats.Errors > 0 {
			line += fmt.Sprintf("  %d errors", stats.Errors)
			for _, failure := range stats.Failures {
				line += fmt.Sprintf(" (%s %d)", failure.Outcome, failure.Count)
			}
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func humanBytes(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%dB", bytes)
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.1fKB", float64(bytes)/1024)
	}
	return fmt.Sprintf("%.1fMB", float64(bytes)/(1024*1024))
}

// grep renders matches in `grep -n -A` shape: the form the bash fallback had,
// and the cheapest to read. Context lines are indented under their match so a
// caller can tell a hit from its surroundings without a second column.
func grep(r protocol.GrepResponse) string {
	if len(r.Matches) == 0 {
		if r.Summary != "" {
			return r.Summary
		}
		return "no matches"
	}

	lines := make([]string, 0, len(r.Matches)+1)
	if r.Summary != "" {
		lines = append(lines, r.Summary)
	}
	for _, match := range r.Matches {
		lines = append(lines, fmt.Sprintf("%s:%d: %s", match.Path, match.Line, match.Text))
		for _, after := range match.After {
			lines = append(lines, "    "+after)
		}
	}
	return strings.Join(lines, "\n")
}

// runCommand renders a declared command's verdict, or the registry listing
// when no command was named.
func runCommand(r protocol.RunCommandResponse) string {
	if r.Status == "listed" || (r.Name == "" && len(r.Available) > 0) {
		return declaredListing(r.Summary, r.Available)
	}

	verdict := "FAIL"
	if r.Passed {
		verdict = "pass"
	}
	if r.Status != "completed" && r.Status != "" {
		verdict = r.Status
	}

	head := strings.TrimSpace(verdict + " " + r.Name)
	if r.Name == "" {
		head = "no commands declared"
	}

	// The shell string is echoed only when the command failed. On success it
	// is noise the caller already knows; on failure it is the first thing they
	// need in order to tell a broken command from broken code.
	if !r.Passed && r.Run != "" {
		head += " · " + r.Run
	}

	if r.Summary == "" {
		return head
	}
	return head + "\n" + r.Summary
}

func declaredListing(summary string, available []protocol.DeclaredCommand) string {
	lines := make([]string, 0, len(available)+1)
	if summary != "" {
		lines = append(lines, summary)
	}
	for _, command := range available {
		line := command.Name + "  " + command.Run
		if command.Description != "" {
			line += "  — " + command.Description
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return "no commands declared"
	}
	return strings.Join(lines, "\n")
}

func declareCommand(r protocol.DeclareCommandResponse) string {
	if r.Name == "" {
		return "no command declared"
	}

	verb := "declared"
	if r.Replaced {
		// Surfaced rather than silent: overwriting a command someone else
		// declared is exactly the change a caller should see in the response.
		verb = "replaced"
	}
	if r.Removed {
		return fmt.Sprintf("removed %s · %s · %d commands", r.Name, r.Path, len(r.Available))
	}

	head := fmt.Sprintf("%s %s = %s", verb, r.Name, r.Run)
	if r.Path != "" {
		head += " · " + r.Path
	}
	return head
}

func check(r protocol.CheckResponse) string {
	// Verdict first: an agent that reads one line should learn whether the
	// check passed, not which job ran it.
	verdict := "FAIL"
	if r.Passed {
		verdict = "pass"
	}
	if r.Status != "completed" {
		verdict = r.Status
	}

	head := strings.TrimSpace(fmt.Sprintf("%s %s", verdict, r.Kind))
	if head == "" {
		head = "(no check)"
	}
	if r.Summary == "" {
		return head
	}
	return head + "\n" + r.Summary
}

func runTests(r protocol.RunTestsResponse) string {
	if r.Status != "completed" {
		head := fmt.Sprintf("running %s", r.JobID)
		if r.Summary != "" {
			return head + "\n" + r.Summary
		}
		return head
	}

	verdict := "FAIL"
	if r.Passed {
		verdict = "pass"
	}
	head := verdict + " tests " + r.JobID
	if r.Summary == "" {
		return head
	}
	return head + "\n" + r.Summary
}

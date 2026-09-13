package code

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/julianbei/jade/internal/protocol"
)

// maxFindBodyLines bounds each returned body. find answers "show me this
// thing", not "dump the file" — a caller wanting the whole declaration can
// follow up with read_symbol.
const maxFindBodyLines = 40

// FindSymbols locates declarations by name and returns their bodies in one
// call.
//
// This closes the most-cited reason to leave jade for the shell. Answering
// "show me the function I have not located yet" took `outline` then
// `read_symbol` — two round trips — while `grep -n "func X" -A 30` fuses
// search and read into one. The shell won that comparison every time, and a
// bash fallback means no telemetry, no guardrails and no revision tracking.
//
// Matching is by symbol name, exact first and then substring, so a caller
// who knows the exact name never has partial matches crowd out the one they
// asked for.
func (i *Index) FindSymbols(query string, kind string, limit int, maxLines int) (protocol.FindResponse, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return protocol.FindResponse{}, fmt.Errorf("query is required")
	}
	if limit <= 0 {
		limit = 5
	}
	if maxLines <= 0 {
		maxLines = maxFindBodyLines
	}
	kind = strings.TrimSpace(strings.ToLower(kind))

	exact, partial, err := i.collectMatches(query, kind)
	if err != nil {
		return protocol.FindResponse{}, err
	}

	// Exact matches first and, when any exist, exclusively: a caller asking
	// for "Apply" wants Apply, not ApplyRequest and applySummary alongside it.
	matches := exact
	if len(matches) == 0 {
		matches = partial
	}

	total := len(matches)
	if len(matches) > limit {
		matches = matches[:limit]
	}

	results := make([]protocol.FindResult, 0, len(matches))
	for _, match := range matches {
		results = append(results, i.buildFindResult(match, maxLines))
	}

	return protocol.FindResponse{
		Query:   query,
		Results: results,
		Total:   total,
		Summary: findSummary(query, total, len(results)),
	}, nil
}

type findMatch struct {
	symbol Symbol
	rel    string
	lines  []string
}

func (i *Index) collectMatches(query string, kind string) ([]findMatch, []findMatch, error) {
	lowered := strings.ToLower(query)
	exact := make([]findMatch, 0, 4)
	partial := make([]findMatch, 0, 8)

	err := filepath.Walk(i.root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info == nil || info.IsDir() {
			return nil
		}
		if shouldSkipPath(path) || !isTextLike(path) || i.leavesWorkspace(path, info) {
			return nil
		}
		rel, err := filepath.Rel(i.root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		lines := splitLines(string(data))
		symbols, _ := i.symbolsForPath(path, rel, data, lines)

		for _, symbol := range symbols {
			if !kindMatches(symbol.Kind, kind) {
				continue
			}
			name := strings.ToLower(symbol.Name)
			match := findMatch{symbol: symbol, rel: rel, lines: lines}
			switch {
			case name == lowered:
				exact = append(exact, match)
			case strings.Contains(name, lowered):
				partial = append(partial, match)
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	sortMatches(exact)
	sortMatches(partial)
	return exact, partial, nil
}

// kindMatches compares a requested kind against jade's internal vocabulary,
// which is not what a caller would guess: a Go struct is reported as
// "class", a Go func as "function". Requiring the caller to know that would
// make the filter silently return nothing for the natural spellings, so
// "type", "struct" and "class" are treated as one family, and "func",
// "fn" and "function" as another.
func kindMatches(actual string, requested string) bool {
	if requested == "" {
		return true
	}
	actual = strings.ToLower(actual)
	requested = strings.ToLower(requested)
	if actual == requested {
		return true
	}

	families := [][]string{
		{"type", "struct", "class", "interface", "enum"},
		{"func", "fn", "function"},
		{"method"},
		{"const", "constant"},
		{"var", "variable"},
	}
	for _, family := range families {
		if contains(family, actual) && contains(family, requested) {
			return true
		}
	}
	return false
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// sortMatches orders by name then path then line, so repeated calls return
// the same order — an unstable result makes a response undiffable and a
// re-read look like a change.
func sortMatches(matches []findMatch) {
	sort.Slice(matches, func(a, b int) bool {
		if matches[a].symbol.Name != matches[b].symbol.Name {
			return matches[a].symbol.Name < matches[b].symbol.Name
		}
		if matches[a].rel != matches[b].rel {
			return matches[a].rel < matches[b].rel
		}
		return matches[a].symbol.From < matches[b].symbol.From
	})
}

func (i *Index) buildFindResult(match findMatch, maxLines int) protocol.FindResult {
	from := match.symbol.From - 1
	if from < 0 {
		from = 0
	}
	to := match.symbol.To
	if to > len(match.lines) {
		to = len(match.lines)
	}

	body := ""
	truncated := false
	if from < to {
		selected := match.lines[from:to]
		if len(selected) > maxLines {
			selected = selected[:maxLines]
			truncated = true
		}
		body = strings.Join(selected, "\n")
	}

	return protocol.FindResult{
		SymbolID:  match.symbol.ID,
		Symbol:    match.symbol.Name,
		Kind:      match.symbol.Kind,
		Path:      match.rel,
		StartLine: match.symbol.From,
		EndLine:   match.symbol.To,
		Body:      body,
		Truncated: truncated,
	}
}

func findSummary(query string, total int, shown int) string {
	if total == 0 {
		return fmt.Sprintf("no declarations matching %q", query)
	}
	if shown < total {
		return fmt.Sprintf("%d matches for %q (showing %d)", total, query, shown)
	}
	return fmt.Sprintf("%d matches for %q", total, query)
}

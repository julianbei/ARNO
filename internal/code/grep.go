package code

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/julianbei/jade/internal/protocol"
)

// Grep is literal or regex text search across the workspace, returning
// matching lines with optional trailing context.
//
// Why this exists, given Search. Search ranks *symbols* by name similarity.
// Asked for the exact string "ChangedPaths" it returns ChangedFile,
// CheckRequest and CommitInfo — declarations whose names merely look alike,
// none of which contain the string, while missing both the struct field
// actually called ChangedPaths and every file that reads it. That is not a
// weak answer to the question; it is a confident answer to a different one,
// which is worse, because a caller who tests it once learns to distrust it and
// goes back to grep permanently.
//
// The questions Search structurally cannot answer, and which recurred in this
// project's own dogfooding log for three tasks running:
//
//   - "What reads ChangesResponse.Paths?" A struct field is not a symbol with
//     a resolvable position, so neither Search nor References reaches it — and
//     this is the question every field removal starts with.
//   - "Where is this string literal / error message / build tag?"
//   - "Show me every match, minus the ones under testdata." Negative filters.
//
// The response is shaped like `grep -n -A`, because that is the shape the
// fallback had and the one that reads cheapest: path:line, the matching line,
// and only as much trailing context as was asked for.
func (i *Index) Grep(req protocol.GrepRequest) (protocol.GrepResponse, error) {
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return protocol.GrepResponse{}, fmt.Errorf("search text is required")
	}

	matcher, err := buildMatcher(query, req.Regex, req.IgnoreCase)
	if err != nil {
		return protocol.GrepResponse{}, err
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 40
	}
	context := req.Context
	if context < 0 {
		context = 0
	}
	if context > maxGrepContext {
		context = maxGrepContext
	}

	matches := make([]protocol.GrepMatch, 0, limit)
	total := 0
	files := map[string]bool{}

	walkErr := filepath.Walk(i.root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			// One unreadable directory must not fail the whole search.
			return nil
		}
		if info == nil || info.IsDir() {
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
		if !pathAllowed(rel, req.Glob, req.Exclude) {
			return nil
		}

		fileMatches, count := grepFile(path, rel, matcher, context, limit-len(matches))
		if count > 0 {
			files[rel] = true
		}
		total += count
		matches = append(matches, fileMatches...)
		return nil
	})
	if walkErr != nil {
		return protocol.GrepResponse{}, walkErr
	}

	// Sorted by path then line so a repeated search returns the same answer.
	// An unstable order makes a response undiffable and a re-read look like a
	// change.
	sort.Slice(matches, func(a, b int) bool {
		if matches[a].Path != matches[b].Path {
			return matches[a].Path < matches[b].Path
		}
		return matches[a].Line < matches[b].Line
	})

	response := protocol.GrepResponse{
		Query:     query,
		Matches:   matches,
		Total:     total,
		Files:     len(files),
		Truncated: total > len(matches),
	}
	response.Summary = grepSummary(response)
	return response, nil
}

// maxGrepContext bounds trailing context. `grep -A 30` is a normal thing to
// type, but an unbounded value on a query with many hits would return the
// whole repository.
const maxGrepContext = 40

// maxGrepLineLength truncates a single very long line — a minified bundle or
// an embedded blob — so one pathological file cannot dominate the response.
const maxGrepLineLength = 400

func grepFile(absolute string, rel string, matcher func(string) bool, context int, remaining int) ([]protocol.GrepMatch, int) {
	if remaining <= 0 {
		// Still count matches past the limit: a cap must cost detail, never
		// accuracy about how many there are.
		remaining = 0
	}

	file, err := os.Open(absolute)
	if err != nil {
		return nil, 0
	}
	defer file.Close()

	// Binary files are skipped, as grep -I and ripgrep do. isTextLike only
	// knows extensions, so a built binary with none (bin/jade-mcp) was
	// searched and returned kilobytes of runtime strings. A NUL byte in the
	// first block is the same test git uses.
	head := make([]byte, 8000)
	n, _ := file.Read(head)
	if bytes.IndexByte(head[:n], 0) >= 0 {
		return nil, 0
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, 0
	}

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	lines := make([]string, 0, 256)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanner.Err() != nil {
		return nil, 0
	}

	matches := make([]protocol.GrepMatch, 0)
	count := 0
	for index, line := range lines {
		if !matcher(line) {
			continue
		}
		count++
		if len(matches) >= remaining {
			continue
		}

		match := protocol.GrepMatch{
			Path: rel,
			Line: index + 1,
			Text: clipLine(line),
		}
		for offset := 1; offset <= context && index+offset < len(lines); offset++ {
			match.After = append(match.After, clipLine(lines[index+offset]))
		}
		matches = append(matches, match)
	}
	return matches, count
}

func clipLine(line string) string {
	if len(line) <= maxGrepLineLength {
		return line
	}
	return line[:maxGrepLineLength] + "…"
}

// buildMatcher compiles the query once rather than per line. A bad regex is
// reported as an error naming the pattern, not silently treated as a literal:
// silently matching something other than what was asked for is the failure
// mode that makes a search tool untrustworthy.
// grepToRE2 reads the escapes agents type from grep habit the way grep reads
// them. `a\|b` is alternation to grep but a literal pipe to Go's regexp, so a
// pattern written for `grep -n` silently matched nothing; a benchmark run spent
// three turns on exactly that before splitting the search by hand. `\|`, `\(`,
// `\)`, `\+` and `\?` become their RE2 operators.
//
// Only for a pattern written that way: one that uses `\|` and has no bare `|`.
// A pattern already in RE2 form — `func (Read|Other)\(` — relies on `\(`
// being a literal paren and is left exactly as written.
func grepToRE2(pattern string) string {
	if !strings.Contains(pattern, `\|`) || strings.Contains(strings.ReplaceAll(pattern, `\|`, ""), "|") {
		return pattern
	}
	return strings.NewReplacer(`\|`, `|`, `\(`, `(`, `\)`, `)`, `\+`, `+`, `\?`, `?`).Replace(pattern)
}

func buildMatcher(query string, isRegex bool, ignoreCase bool) (func(string) bool, error) {
	if isRegex {
		pattern := grepToRE2(query)
		if ignoreCase {
			pattern = "(?i)" + pattern
		}
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid regex %q: %w", query, err)
		}
		return compiled.MatchString, nil
	}

	if ignoreCase {
		lowered := strings.ToLower(query)
		return func(line string) bool {
			return strings.Contains(strings.ToLower(line), lowered)
		}, nil
	}
	return func(line string) bool {
		return strings.Contains(line, query)
	}, nil
}

// pathAllowed applies the include glob and the exclude filter.
//
// The glob is matched against both the base name and the full relative path,
// so "*.go" and "internal/code/*" both work — a caller should not have to know
// which of the two spellings this implementation prefers.
//
// Exclude is a plain substring on the path rather than a glob. It exists
// because the bash form it replaces was invariably `| grep -v testdata`, and
// a substring is what that actually did.
func pathAllowed(rel string, glob string, exclude string) bool {
	if exclude != "" && strings.Contains(rel, exclude) {
		return false
	}
	if glob == "" {
		return true
	}
	if matched, err := filepath.Match(glob, filepath.Base(rel)); err == nil && matched {
		return true
	}
	if matched, err := filepath.Match(glob, rel); err == nil && matched {
		return true
	}
	return false
}

func grepSummary(r protocol.GrepResponse) string {
	if r.Total == 0 {
		return fmt.Sprintf("no matches for %q", r.Query)
	}
	summary := fmt.Sprintf("%d matches in %d files", r.Total, r.Files)
	if r.Truncated {
		summary += fmt.Sprintf(", showing %d — raise limit or narrow with glob/exclude", len(r.Matches))
	}
	return summary
}

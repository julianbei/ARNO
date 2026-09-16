package code

import (
	"fmt"
	"regexp"
	"strings"
)

// SearchNudge implements the search-nudge idea: rather than trying to make
// an agent choose arno's search tool over a shell grep/find call,
// piggyback a few relevant index hits onto the tool call it already made.
//
// ARNO is an MCP server, not the agent harness, so it cannot itself observe
// or intercept a Bash/Grep/Glob tool call — that interception has to happen
// in the harness (e.g. a PostToolUse hook). What ARNO *can* do, and what
// this provides, is the reusable piece: given the raw command string the
// harness already saw, decide whether a nudge is warranted and produce the
// footer text to append. A harness integration calls this (e.g. via the
// arno.search_nudge MCP tool) after a matching shell command; ARNO never
// sees or needs to see the tool call itself.
const (
	nudgeMinOutputChars = 1200
	nudgeMaxHits        = 3
)

var (
	locatorCommandPattern = regexp.MustCompile(`(?i)^(grep|rg|ag|ack|find|fd)\b`)
	flagValuePattern      = regexp.MustCompile(`-(?:name|iname|path)\s+(?:"([^"]+)"|'([^']+)'|(\S+))`)
	quotedStringPattern   = regexp.MustCompile(`"([^"]+)"|'([^']+)'`)
	metaCharPattern       = regexp.MustCompile(`[.^$*+?()\[\]{}|\\]`)
	letterRunPattern      = regexp.MustCompile(`[A-Za-z]{4,}`)
)

// SearchNudge decides whether a nudge is warranted for command (a raw shell
// command string) given how much output it produced and whether this is the
// first search of the session, and if so returns the footer text to append
// below that command's own output. It never replaces or filters that
// output — ok=false simply means: don't append anything.
func (i *Index) SearchNudge(command string, outputLength int, firstInSession bool) (footer string, ok bool) {
	if outputLength < nudgeMinOutputChars && !firstInSession {
		return "", false
	}

	query, found := locateQuery(command)
	if !found {
		return "", false
	}
	query, found = cleanNudgeQuery(query)
	if !found {
		return "", false
	}

	resp, err := i.Search(query, "auto", nudgeMaxHits)
	if err != nil || len(resp.Hits) == 0 {
		return "", false
	}

	lines := make([]string, 0, len(resp.Hits))
	for _, hit := range resp.Hits {
		lines = append(lines, fmt.Sprintf("%s:%d-%d", hit.Path, hit.StartLine, hit.EndLine))
	}
	return fmt.Sprintf("arno index hits for %q:\n%s", query, strings.Join(lines, "\n")), true
}

// locateQuery extracts the search pattern from a shell command that invokes
// a locator binary (grep/rg/ag/ack/find/fd), preferring, in order: a
// -name/-iname/-path flag's value, a quoted string, or the first bare
// (non-flag) argument after the binary name.
func locateQuery(command string) (string, bool) {
	segment, found := locateSegment(command)
	if !found {
		return "", false
	}

	if match := flagValuePattern.FindStringSubmatch(segment); match != nil {
		for _, group := range match[1:] {
			if group != "" {
				return group, true
			}
		}
	}

	if match := quotedStringPattern.FindStringSubmatch(segment); match != nil {
		for _, group := range match[1:] {
			if group != "" {
				return group, true
			}
		}
	}

	fields := strings.Fields(segment)
	for _, field := range fields[1:] {
		if strings.HasPrefix(field, "-") {
			continue
		}
		return field, true
	}
	return "", false
}

// locateSegment splits command on shell separators (|, ;, &) that are not
// inside quotes, and returns the first segment that starts with a locator
// binary — mirroring a piped or chained command like
// `grep -r foo . | wc -l` or `cd src && rg bar`.
func locateSegment(command string) (string, bool) {
	for _, segment := range splitShellSegments(command) {
		trimmed := strings.TrimSpace(segment)
		if locatorCommandPattern.MatchString(trimmed) {
			return trimmed, true
		}
	}
	return "", false
}

func splitShellSegments(command string) []string {
	segments := make([]string, 0, 4)
	var current strings.Builder
	inSingle, inDouble := false, false
	for _, r := range command {
		switch {
		case r == '\'' && !inDouble:
			inSingle = !inSingle
			current.WriteRune(r)
		case r == '"' && !inSingle:
			inDouble = !inDouble
			current.WriteRune(r)
		case (r == '|' || r == ';' || r == '&') && !inSingle && !inDouble:
			segments = append(segments, current.String())
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	segments = append(segments, current.String())
	return segments
}

// cleanNudgeQuery rejects patterns unlikely to be a meaningful search term:
// mostly regex metacharacters, too short once syntax is stripped, or with
// no run of 4+ letters at all (filters out things like "^\|\?W[0-9]").
func cleanNudgeQuery(pattern string) (string, bool) {
	trimmed := strings.TrimSpace(pattern)
	if trimmed == "" {
		return "", false
	}

	metaCount := len(metaCharPattern.FindAllString(trimmed, -1))
	if float64(metaCount)/float64(len(trimmed)) > 0.25 {
		return "", false
	}

	stripped := metaCharPattern.ReplaceAllString(trimmed, "")
	if len(stripped) < 3 {
		return "", false
	}

	if !letterRunPattern.MatchString(trimmed) {
		return "", false
	}

	return trimmed, true
}

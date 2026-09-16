package workspace

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/julianbei/arno/internal/protocol"
	"github.com/julianbei/arno/internal/textutil"
)

// maxHistoryPatchBytes bounds the optional patch body, for the same reason
// 8.1 bounds job output: a long-lived symbol's full history is unbounded and
// would be the largest response arno can produce.
const maxHistoryPatchBytes = 6000

// defaultHistoryLimit keeps the common case cheap. docs/scope.md §18's framing is
// "why does this code exist", and the answer is almost always in the most
// recent handful of commits.
const defaultHistoryLimit = 10

// historyFieldSeparator is ASCII unit-separator: commit subjects routinely
// contain every printable character a more obvious separator would use, and
// a control character cannot collide with one.
//
// NUL would be the conventional choice but cannot be used here — it is not
// legal in an exec argument, and passing it produced
// "fork/exec git: invalid argument". Instead the format string contains the
// literal text "%x1f", which git itself expands to the separator byte, so
// no control character ever appears in argv.
const historyFieldSeparator = "\x1f"
const historyFieldSeparatorFormat = "%x1f"

// SymbolHistory returns the commits that touched one line range —
// docs/scope.md §18's `history(SessionManager.refreshSession)` instead of
// `git log -p` over a whole file.
//
// This is `git log -L <start>,<end>:<file>`, a real git primitive that
// follows the range backwards through history rather than a line-number
// guess against old revisions. Without a patch it emits commits only, which
// is the progressive-disclosure default: the commit list answers "why does
// this exist", and the patch is the follow-up question.
func (m *Manager) SymbolHistory(path string, startLine int, endLine int, limit int, includePatch bool) (protocol.HistoryResponse, error) {
	response, patch, err := m.SymbolHistoryPatch(path, startLine, endLine, limit, includePatch)
	if err != nil || !includePatch {
		return response, err
	}
	response.Patch, response.OmittedBytes = textutil.Clamp(patch, maxHistoryPatchBytes,
		"request fewer commits with limit to see more of each")
	return response, nil
}

// SymbolHistoryPatch is SymbolHistory with the patch returned whole beside the
// response, for a caller that pages it instead of cutting its middle.
func (m *Manager) SymbolHistoryPatch(path string, startLine int, endLine int, limit int, includePatch bool) (protocol.HistoryResponse, string, error) {
	if startLine < 1 || endLine < startLine {
		return protocol.HistoryResponse{}, "", fmt.Errorf("invalid line range %d,%d", startLine, endLine)
	}
	if limit <= 0 {
		limit = defaultHistoryLimit
	}

	clean := filepath.ToSlash(filepath.Clean(path))
	args := []string{
		"log",
		fmt.Sprintf("-L%d,%d:%s", startLine, endLine, clean),
		fmt.Sprintf("-%d", limit),
		"--date=short",
	}
	if includePatch {
		args = append(args, historyFormat())
	} else {
		// -s suppresses the diff that -L otherwise always emits.
		args = append(args, "-s", historyFormat())
	}

	stdout, err := m.gitRaw(args...)
	if err != nil {
		return protocol.HistoryResponse{}, "", fmt.Errorf("history unavailable for %s:%d-%d: %w", clean, startLine, endLine, err)
	}

	commits, patch := parseHistory(stdout, includePatch)
	response := protocol.HistoryResponse{
		Path:      clean,
		StartLine: startLine,
		EndLine:   endLine,
		Commits:   commits,
	}
	response.Summary = historySummary(clean, startLine, endLine, commits)
	return response, patch, nil
}

// parseHistory separates the commit header lines from the diff bodies. A
// header is identified by carrying the separators this command asked for, so
// a diff line that merely looks like a header cannot be mistaken for one.
func parseHistory(output string, collectPatch bool) ([]protocol.CommitInfo, string) {
	commits := make([]protocol.CommitInfo, 0)
	var patch strings.Builder

	for _, line := range strings.Split(output, "\n") {
		fields := strings.Split(line, historyFieldSeparator)
		if len(fields) == 4 {
			commits = append(commits, protocol.CommitInfo{
				SHA:     shortSHA(fields[0]),
				Author:  fields[1],
				Date:    fields[2],
				Subject: fields[3],
			})
			continue
		}
		if collectPatch {
			patch.WriteString(line)
			patch.WriteString("\n")
		}
	}
	return commits, strings.TrimSpace(patch.String())
}

// shortSHA trims to the 7 characters git itself abbreviates to. The full SHA
// costs 33 more characters per commit and answers no question the short one
// does not.
func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// historyFormat builds git's --format with git-expanded separators.
func historyFormat() string {
	sep := historyFieldSeparatorFormat
	return "--format=%H" + sep + "%an" + sep + "%ad" + sep + "%s"
}

func historySummary(path string, startLine int, endLine int, commits []protocol.CommitInfo) string {
	if len(commits) == 0 {
		return fmt.Sprintf("no commits touched %s:%d-%d", path, startLine, endLine)
	}
	return fmt.Sprintf("%d commits touched %s:%d-%d (most recent %s)",
		len(commits), path, startLine, endLine, commits[0].Date)
}

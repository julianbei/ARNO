package diagnostics

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/julianbei/jade/internal/protocol"
)

// LanguageServerCheck checks a file with its language server. It returns the
// diagnostics, the name of the server that produced them, why nothing checked
// the file when nothing did, and whether the file has a language at all.
// Supplied by the caller so this package does not depend on the code index.
type LanguageServerCheck func(path string) (diagnostics []protocol.Diagnostic, checker string, unchecked string, handled bool)

// UseLanguageServers enables language-server diagnostics for every language
// with a configured server.
func (s *Service) UseLanguageServers(check LanguageServerCheck) {
	s.languageServers = check
}

// Result is one file's check: what was found, and what did the finding.
//
// Checker and Unchecked exist because an empty diagnostic list is ambiguous.
// Thirteen new TypeScript files produced no diagnostics and no way to tell
// "nothing wrong" from "nothing looked". Exactly one of them is set for a file
// Jade knows the language of; both are empty for files with no language
// (Markdown, a Dockerfile), where "not checked" on every edit would be noise.
type Result struct {
	Diagnostics []protocol.Diagnostic
	Checker     string
	Unchecked   string
}

type checkMemo struct {
	modified time.Time
	size     int64
	result   Result
}

// Check runs the synchronous checks for the file named by scope.
//
// Results are memoised per file on modification time and size. Edit responses
// ask for diagnostics and for the checker name separately, and running gopls
// or waiting on a language server twice for one unchanged file would double
// the latency of every edit.
func (s *Service) Check(scope string) Result {
	path := scopePath(scope)
	if path == "" {
		return Result{}
	}
	absolute := path
	if !filepath.IsAbs(absolute) {
		absolute = filepath.Join(s.root, path)
	}

	info, statErr := os.Stat(absolute)
	if statErr == nil {
		s.memoMu.Lock()
		cached, ok := s.memo[absolute]
		s.memoMu.Unlock()
		if ok && cached.modified.Equal(info.ModTime()) && cached.size == info.Size() {
			return cached.result
		}
	}

	var result Result
	switch {
	case strings.EqualFold(filepath.Ext(path), ".go"):
		result = s.checkGo(path, absolute)
	case s.languageServers != nil:
		diagnostics, checker, unchecked, handled := s.languageServers(path)
		if handled {
			result = Result{Diagnostics: diagnostics, Checker: checker, Unchecked: unchecked}
		}
	}

	if statErr == nil {
		s.memoMu.Lock()
		s.memo[absolute] = checkMemo{modified: info.ModTime(), size: info.Size(), result: result}
		s.memoMu.Unlock()
	}
	return result
}

// Checks reports which checkers ran on the files in scopes, and which files
// went unchecked and why, for an edit response.
func (s *Service) Checks(scopes ...string) protocol.CheckReport {
	checked := make(map[string]bool)
	var report protocol.CheckReport
	for _, scope := range scopes {
		result := s.Check(scope)
		if result.Checker != "" && !checked[result.Checker] {
			checked[result.Checker] = true
			report.Checked = append(report.Checked, result.Checker)
		}
		if result.Unchecked != "" {
			report.Unchecked = append(report.Unchecked, scopePath(scope)+": "+result.Unchecked)
		}
	}
	sort.Strings(report.Checked)
	return report
}

package edit

import (
	"errors"
	"strings"

	"github.com/julianbei/arno/internal/code"
	"github.com/julianbei/arno/internal/diagnostics"
	"github.com/julianbei/arno/internal/jobs"
	"github.com/julianbei/arno/internal/protocol"
	"github.com/julianbei/arno/internal/workspace"
)

// ErrStaleRevision is returned by ReplaceRange when expectedRevision no
// longer matches the workspace's current revision.
var ErrStaleRevision = errors.New("edit rejected: stale revision")

// Service owns mutation operations.
type Service struct {
	workspace *workspace.Manager
	index     *code.Index
	diag      *diagnostics.Service
	jobs      *jobs.Runner
}

func NewService(
	workspaceManager *workspace.Manager,
	index *code.Index,
	diagnosticService *diagnostics.Service,
	runner *jobs.Runner,
) *Service {
	return &Service{
		workspace: workspaceManager,
		index:     index,
		diag:      diagnosticService,
		jobs:      runner,
	}
}

func (s *Service) ReplaceSymbol(symbolID string, expectedRevision string, newCode string) (protocol.EditResponse, error) {
	current := s.workspace.Revision()
	if expectedRevision != "" && expectedRevision != current {
		return protocol.EditResponse{}, ErrStaleRevision
	}

	symbol, oldLines, err := s.index.ReplaceSymbolSource(symbolID, newCode)
	if err != nil {
		return protocol.EditResponse{}, err
	}

	// Report the ID that was actually edited, not the one the caller typed:
	// a bare "greet.go::Greet" resolves to "greet.go::Greet@3", and echoing
	// the input back would hide which declaration moved.
	symbolID = symbol.ID

	formatted := s.formatTouched(symbol.Path)
	oldRev, newRev := s.workspace.BumpRevision(symbolID)

	return protocol.EditResponse{
		OldRevision:  oldRev,
		NewRevision:  newRev,
		Changed:      []string{symbolID},
		AddedLines:   countLines(newCode),
		RemovedLines: len(oldLines),
		Formatted:    formatted,
		Diagnostics:  s.diag.Immediate(symbolID),
		Checks:       s.diag.Checks(symbolID),
	}, nil
}

func (s *Service) ReplaceRange(path string, expectedRevision string, start int, end int, newCode string) (protocol.EditResponse, error) {
	current := s.workspace.Revision()
	if expectedRevision != "" && expectedRevision != current {
		return protocol.EditResponse{}, ErrStaleRevision
	}

	oldLines, err := s.index.ReplaceRangeSource(path, start, end, newCode)
	if err != nil {
		return protocol.EditResponse{}, err
	}

	formatted := s.formatTouched(path)
	oldRev, newRev := s.workspace.BumpRevision(path)

	return protocol.EditResponse{
		OldRevision:  oldRev,
		NewRevision:  newRev,
		Changed:      []string{path},
		AddedLines:   countLines(newCode),
		RemovedLines: len(oldLines),
		Formatted:    formatted,
		Diagnostics:  s.diag.Immediate(path),
		Checks:       s.diag.Checks(path),
		Snippets:     snippets(s.workspace.Root(), path, newCode),
	}, nil
}

// ReplaceText replaces an exact, unique string — the string-anchored
// counterpart to ReplaceRange. It carries the same revision precondition:
// anchoring by text removes the line-drift problem, not the concurrent-edit
// problem.
func (s *Service) ReplaceText(path string, expectedRevision string, oldText string, newText string) (protocol.EditResponse, error) {
	current := s.workspace.Revision()
	if expectedRevision != "" && expectedRevision != current {
		return protocol.EditResponse{}, ErrStaleRevision
	}

	if _, err := s.index.ReplaceTextSource(path, oldText, newText); err != nil {
		return protocol.EditResponse{}, err
	}
	addedLines, removedLines := lineDelta(oldText, newText)

	formatted := s.formatTouched(path)
	oldRev, newRev := s.workspace.BumpRevision(path)

	return protocol.EditResponse{
		OldRevision:  oldRev,
		NewRevision:  newRev,
		Changed:      []string{path},
		AddedLines:   addedLines,
		RemovedLines: removedLines,
		Formatted:    formatted,
		Diagnostics:  s.diag.Immediate(path),
		Checks:       s.diag.Checks(path),
		Snippets:     snippets(s.workspace.Root(), path, newText),
	}, nil
}

// Insert adds text without replacing anything — the additive counterpart to
// ReplaceText, carrying the same revision precondition.
//
// It existed only as an `apply` op until now, which meant additive work (a new
// test function, a section appended to a document) had no tool of its own and
// was reported as faster to do with a plain file edit. That is the same
// failure mode the op was written for: when ARNO has no cheap way to add
// something, adding it happens somewhere ARNO cannot see.
func (s *Service) Insert(path string, expectedRevision string, anchor string, position string, text string) (protocol.EditResponse, error) {
	current := s.workspace.Revision()
	if expectedRevision != "" && expectedRevision != current {
		return protocol.EditResponse{}, ErrStaleRevision
	}

	addedLines, err := s.index.InsertSource(path, anchor, position, text)
	if err != nil {
		return protocol.EditResponse{}, err
	}

	formatted := s.formatTouched(path)
	oldRev, newRev := s.workspace.BumpRevision(path)

	return protocol.EditResponse{
		OldRevision:  oldRev,
		NewRevision:  newRev,
		Changed:      []string{path},
		AddedLines:   addedLines,
		RemovedLines: 0,
		Formatted:    formatted,
		Diagnostics:  s.diag.Immediate(path),
		Checks:       s.diag.Checks(path),
		Snippets:     snippets(s.workspace.Root(), path, text),
	}, nil
}

// DeleteSymbol removes a declaration entirely, with the same revision
// precondition as every other mutation.
func (s *Service) DeleteSymbol(path string, symbolID string, expectedRevision string) (protocol.EditResponse, error) {
	current := s.workspace.Revision()
	if expectedRevision != "" && expectedRevision != current {
		return protocol.EditResponse{}, ErrStaleRevision
	}

	symbol, removed, err := s.index.DeleteSymbolSource(path, symbolID)
	if err != nil {
		return protocol.EditResponse{}, err
	}

	formatted := s.formatTouched(symbol.Path)
	oldRev, newRev := s.workspace.BumpRevision(symbol.Path)

	return protocol.EditResponse{
		OldRevision:  oldRev,
		NewRevision:  newRev,
		Changed:      []string{symbol.Path},
		RemovedLines: len(removed),
		Formatted:    formatted,
		Diagnostics:  s.diag.Immediate(symbol.Path),
		Checks:       s.diag.Checks(symbol.Path),
	}, nil
}

// CreateFile writes a brand-new file — docs/scope.md §29's MVP editing operation
// that, alongside DeleteFile, had no implementation at all until now. No
// revision precondition applies (there is nothing prior to be stale
// relative to); CreateFile itself refuses to overwrite an existing file.
func (s *Service) CreateFile(path string, content string) (protocol.EditResponse, error) {
	if err := s.index.CreateFile(path, content); err != nil {
		return protocol.EditResponse{}, err
	}

	formatted := s.formatTouched(path)
	oldRev, newRev := s.workspace.BumpRevision(path)

	return protocol.EditResponse{
		OldRevision: oldRev,
		NewRevision: newRev,
		Changed:     []string{path},
		AddedLines:  countLines(content),
		Formatted:   formatted,
		Diagnostics: s.diag.Immediate(path),
		Checks:      s.diag.Checks(path),
	}, nil
}

// ReplaceFile overwrites an existing file's entire contents.
//
// Unlike CreateFile it reports RemovedLines, because the caller is destroying
// content and the size of what was destroyed is the one fact they cannot
// recover from the response otherwise.
func (s *Service) ReplaceFile(path string, content string) (protocol.EditResponse, error) {
	removedLines, err := s.index.ReplaceFileSource(path, content)
	if err != nil {
		return protocol.EditResponse{}, err
	}

	formatted := s.formatTouched(path)
	oldRev, newRev := s.workspace.BumpRevision(path)

	return protocol.EditResponse{
		OldRevision:  oldRev,
		NewRevision:  newRev,
		Changed:      []string{path},
		AddedLines:   countLines(content),
		RemovedLines: removedLines,
		Formatted:    formatted,
		Diagnostics:  s.diag.Immediate(path),
		Checks:       s.diag.Checks(path),
	}, nil
}

// DeleteFile removes a file — the other half of docs/scope.md §29's MVP gap.
func (s *Service) DeleteFile(path string) (protocol.EditResponse, error) {
	removedLines, err := s.index.DeleteFile(path)
	if err != nil {
		return protocol.EditResponse{}, err
	}

	oldRev, newRev := s.workspace.BumpRevision(path)

	return protocol.EditResponse{
		OldRevision:  oldRev,
		NewRevision:  newRev,
		Changed:      []string{path},
		RemovedLines: removedLines,
	}, nil
}

// Rename renames a symbol repository-wide via gopls (7.2). It differs from
// every other mutation here in that ARNO does not compute the edit: the
// language server does, and arno's job is the revision precondition, the
// changed-file accounting and the validation job that follows. Because a
// rename touches files the caller never named, the revision check matters
// more here than anywhere else.
func (s *Service) Rename(path string, symbolID string, newName string, expectedRevision string) (protocol.EditResponse, error) {
	current := s.workspace.Revision()
	if expectedRevision != "" && expectedRevision != current {
		return protocol.EditResponse{}, ErrStaleRevision
	}

	changed, err := s.index.RenameSymbol(path, symbolID, newName)
	if err != nil {
		return protocol.EditResponse{}, err
	}

	// A rename touches files the caller never named, so every one of them
	// gets formatted, not just the declaring file.
	formatted := s.formatTouched(changed...)
	oldRev, newRev := s.workspace.BumpRevision(symbolID)

	return protocol.EditResponse{
		OldRevision: oldRev,
		NewRevision: newRev,
		Changed:     changed,
		Formatted:   formatted,
		Diagnostics: s.diag.Immediate(path),
		Checks:      s.diag.Checks(path),
	}, nil
}

// Single edits start no background validation job. Each one used to start a
// whole-repository typecheck (a full test run for replace_range) whose result
// no response showed: diagnostics already come back inline, and check or
// apply's check runs validation when the caller asks. The jobs cost CPU on
// every edit and distorted every benchmark timing.

// lineDelta counts the lines a text replacement actually adds and removes,
// ignoring lines the old and new text share at the start and end. Appending
// 14 lines after a 4-line anchor is +14 -0, not +18 -4: the anchor was kept,
// and counting it as rewritten made a pure addition read as a rewrite.
func lineDelta(oldText string, newText string) (int, int) {
	split := func(text string) []string {
		trimmed := strings.TrimSuffix(text, "\n")
		if trimmed == "" {
			return nil
		}
		return strings.Split(trimmed, "\n")
	}
	oldLines, newLines := split(oldText), split(newText)

	prefix := 0
	for prefix < len(oldLines) && prefix < len(newLines) && oldLines[prefix] == newLines[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(oldLines)-prefix && suffix < len(newLines)-prefix &&
		oldLines[len(oldLines)-1-suffix] == newLines[len(newLines)-1-suffix] {
		suffix++
	}
	return len(newLines) - prefix - suffix, len(oldLines) - prefix - suffix
}

func countLines(s string) int {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return 0
	}
	return strings.Count(trimmed, "\n") + 1
}

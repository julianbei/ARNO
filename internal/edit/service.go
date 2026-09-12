package edit

import (
	"errors"
	"strings"

	"github.com/julianbei/jade/internal/code"
	"github.com/julianbei/jade/internal/diagnostics"
	"github.com/julianbei/jade/internal/jobs"
	"github.com/julianbei/jade/internal/protocol"
	"github.com/julianbei/jade/internal/workspace"
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
	jobID := s.jobs.Start("typecheck")
	s.jobs.RunValidationCommand(jobID, s.workspace.Root(), "typecheck")

	return protocol.EditResponse{
		OldRevision:  oldRev,
		NewRevision:  newRev,
		Changed:      []string{symbolID},
		AddedLines:   countLines(newCode),
		RemovedLines: len(oldLines),
		Formatted:    formatted,
		Diagnostics:  s.diag.Immediate(symbolID),
		Jobs:         []string{jobID},
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
	jobID := s.jobs.Start("tests")
	s.jobs.RunValidationCommand(jobID, s.workspace.Root(), "tests")

	return protocol.EditResponse{
		OldRevision:  oldRev,
		NewRevision:  newRev,
		Changed:      []string{path},
		AddedLines:   countLines(newCode),
		RemovedLines: len(oldLines),
		Formatted:    formatted,
		Diagnostics:  s.diag.Immediate(path),
		Jobs:         []string{jobID},
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

	removedLines, err := s.index.ReplaceTextSource(path, oldText, newText)
	if err != nil {
		return protocol.EditResponse{}, err
	}

	formatted := s.formatTouched(path)
	oldRev, newRev := s.workspace.BumpRevision(path)
	jobID := s.jobs.Start("typecheck")
	s.jobs.RunValidationCommand(jobID, s.workspace.Root(), "typecheck")

	return protocol.EditResponse{
		OldRevision:  oldRev,
		NewRevision:  newRev,
		Changed:      []string{path},
		AddedLines:   countLines(newText),
		RemovedLines: removedLines,
		Formatted:    formatted,
		Diagnostics:  s.diag.Immediate(path),
		Jobs:         []string{jobID},
	}, nil
}

// Insert adds text without replacing anything — the additive counterpart to
// ReplaceText, carrying the same revision precondition.
//
// It existed only as an `apply` op until now, which meant additive work (a new
// test function, a section appended to a document) had no tool of its own and
// was reported as faster to do with a plain file edit. That is the same
// failure mode the op was written for: when jade has no cheap way to add
// something, adding it happens somewhere jade cannot see.
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
	jobID := s.jobs.Start("typecheck")
	s.jobs.RunValidationCommand(jobID, s.workspace.Root(), "typecheck")

	return protocol.EditResponse{
		OldRevision:  oldRev,
		NewRevision:  newRev,
		Changed:      []string{path},
		AddedLines:   addedLines,
		RemovedLines: 0,
		Formatted:    formatted,
		Diagnostics:  s.diag.Immediate(path),
		Jobs:         []string{jobID},
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
	jobID := s.jobs.Start("typecheck")
	s.jobs.RunValidationCommand(jobID, s.workspace.Root(), "typecheck")

	return protocol.EditResponse{
		OldRevision:  oldRev,
		NewRevision:  newRev,
		Changed:      []string{symbol.Path},
		RemovedLines: len(removed),
		Formatted:    formatted,
		Diagnostics:  s.diag.Immediate(symbol.Path),
		Jobs:         []string{jobID},
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
	jobID := s.jobs.Start("typecheck")
	s.jobs.RunValidationCommand(jobID, s.workspace.Root(), "typecheck")

	return protocol.EditResponse{
		OldRevision: oldRev,
		NewRevision: newRev,
		Changed:     []string{path},
		AddedLines:  countLines(content),
		Formatted:   formatted,
		Diagnostics: s.diag.Immediate(path),
		Jobs:        []string{jobID},
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
	jobID := s.jobs.Start("typecheck")
	s.jobs.RunValidationCommand(jobID, s.workspace.Root(), "typecheck")

	return protocol.EditResponse{
		OldRevision:  oldRev,
		NewRevision:  newRev,
		Changed:      []string{path},
		AddedLines:   countLines(content),
		RemovedLines: removedLines,
		Formatted:    formatted,
		Diagnostics:  s.diag.Immediate(path),
		Jobs:         []string{jobID},
	}, nil
}

// DeleteFile removes a file — the other half of docs/scope.md §29's MVP gap.
func (s *Service) DeleteFile(path string) (protocol.EditResponse, error) {
	removedLines, err := s.index.DeleteFile(path)
	if err != nil {
		return protocol.EditResponse{}, err
	}

	oldRev, newRev := s.workspace.BumpRevision(path)
	jobID := s.jobs.Start("typecheck")
	s.jobs.RunValidationCommand(jobID, s.workspace.Root(), "typecheck")

	return protocol.EditResponse{
		OldRevision:  oldRev,
		NewRevision:  newRev,
		Changed:      []string{path},
		RemovedLines: removedLines,
		Jobs:         []string{jobID},
	}, nil
}

// Rename renames a symbol repository-wide via gopls (7.2). It differs from
// every other mutation here in that jade does not compute the edit: the
// language server does, and jade's job is the revision precondition, the
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
	jobID := s.jobs.Start("typecheck")
	s.jobs.RunValidationCommand(jobID, s.workspace.Root(), "typecheck")

	return protocol.EditResponse{
		OldRevision: oldRev,
		NewRevision: newRev,
		Changed:     changed,
		Formatted:   formatted,
		Diagnostics: s.diag.Immediate(path),
		Jobs:        []string{jobID},
	}, nil
}

func countLines(s string) int {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return 0
	}
	return strings.Count(trimmed, "\n") + 1
}

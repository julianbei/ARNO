package edit

import (
	"strings"

	"github.com/julianbei/jade/internal/code"
	"github.com/julianbei/jade/internal/diagnostics"
	"github.com/julianbei/jade/internal/jobs"
	"github.com/julianbei/jade/internal/protocol"
	"github.com/julianbei/jade/internal/workspace"
)

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

func (s *Service) ReplaceSymbol(symbolID string, newCode string) protocol.EditResponse {
	oldRev, newRev := s.workspace.BumpRevision(symbolID)
	jobID := s.jobs.Start("typecheck")

	return protocol.EditResponse{
		OldRevision: oldRev,
		NewRevision: newRev,
		Changed:     []string{symbolID},
		AddedLines:  countLines(newCode),
		Diagnostics: s.diag.Immediate(symbolID),
		Jobs:        []string{jobID},
	}
}

func (s *Service) ReplaceRange(path string, expectedRevision string, start int, end int, newCode string) (protocol.EditResponse, bool) {
	current := s.workspace.Revision()
	if expectedRevision != "" && expectedRevision != current {
		return protocol.EditResponse{}, false
	}

	oldRev, newRev := s.workspace.BumpRevision(path)
	jobID := s.jobs.Start("tests")
	return protocol.EditResponse{
		OldRevision: oldRev,
		NewRevision: newRev,
		Changed:     []string{path},
		AddedLines:  countLines(newCode),
		Diagnostics: s.diag.Immediate(path),
		Jobs:        []string{jobID},
	}, true
}

func countLines(s string) int {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return 0
	}
	return strings.Count(trimmed, "\n") + 1
}

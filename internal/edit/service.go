package edit

import (
	"github.com/julianbei/jade/internal/code"
	"github.com/julianbei/jade/internal/protocol"
	"github.com/julianbei/jade/internal/workspace"
)

// Service owns mutation operations.
type Service struct {
	workspace *workspace.Manager
	index     *code.Index
}

func NewService(workspaceManager *workspace.Manager, index *code.Index) *Service {
	return &Service{workspace: workspaceManager, index: index}
}

func (s *Service) ReplaceSymbol(symbolID string, newCode string) protocol.EditResponse {
	oldRev, newRev := s.workspace.BumpRevision(symbolID)

	return protocol.EditResponse{
		OldRevision: oldRev,
		NewRevision: newRev,
		Changed:     []string{symbolID},
	}
}

func (s *Service) ReplaceRange(path string, expectedRevision string, start int, end int, newCode string) (protocol.EditResponse, bool) {
	current := s.workspace.Revision()
	if expectedRevision != "" && expectedRevision != current {
		return protocol.EditResponse{}, false
	}

	oldRev, newRev := s.workspace.BumpRevision(path)
	return protocol.EditResponse{
		OldRevision: oldRev,
		NewRevision: newRev,
		Changed:     []string{path},
	}, true
}

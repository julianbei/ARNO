package diagnostics

import "github.com/julianbei/jade/internal/protocol"

// Service normalizes parser, LSP, and lint diagnostics.
type Service struct{}

func NewService() *Service {
	return &Service{}
}

func (s *Service) Immediate(scope string) []protocol.Diagnostic {
	return nil
}

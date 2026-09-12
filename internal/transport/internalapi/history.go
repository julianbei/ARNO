package internalapi

import (
	"github.com/julianbei/jade/internal/protocol"
)

// History reports which commits touched a symbol — docs/scope.md §18.
//
// The line range comes from jade's current parse of the file, and git's -L
// then follows that range backwards through history itself. That ordering
// matters: jade never has to guess where the symbol lived in an older
// revision, which is the part a naive implementation gets wrong.
func (s *Server) History(req protocol.HistoryRequest) (protocol.HistoryResponse, error) {
	symbolID, err := s.resolveSymbolID(req.Path, req.SymbolID, req.SymbolName)
	if err != nil {
		return protocol.HistoryResponse{}, err
	}

	symbol, _, err := s.index.ReadSymbol(req.Path, symbolID, 0)
	if err != nil {
		return protocol.HistoryResponse{}, err
	}

	return s.workspace.SymbolHistory(symbol.Path, symbol.From, symbol.To, req.Limit, req.IncludePatch)
}

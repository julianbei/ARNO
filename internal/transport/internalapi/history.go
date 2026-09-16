package internalapi

import (
	"github.com/julianbei/arno/internal/protocol"
)

// History reports which commits touched a symbol — docs/scope.md §18.
//
// The line range comes from arno's current parse of the file, and git's -L
// then follows that range backwards through history itself. That ordering
// matters: arno never has to guess where the symbol lived in an older
// revision, which is the part a naive implementation gets wrong.
// historyAll answers history with its patch whole beside the response.
// History pages the patch.
func (s *Server) historyAll(req protocol.HistoryRequest) (protocol.HistoryResponse, string, error) {
	symbolID, err := s.resolveSymbolID(req.Path, req.SymbolID, req.SymbolName)
	if err != nil {
		return protocol.HistoryResponse{}, "", err
	}

	symbol, _, err := s.index.ReadSymbol(req.Path, symbolID, 0)
	if err != nil {
		return protocol.HistoryResponse{}, "", err
	}

	return s.workspace.SymbolHistoryPatch(symbol.Path, symbol.From, symbol.To, req.Limit, req.IncludePatch)
}

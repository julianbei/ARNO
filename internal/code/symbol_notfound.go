package code

import (
	"fmt"
	"strings"

	"github.com/julianbei/arno/internal/protocol"
)

// symbolNotFoundError builds the error for an ID that matched no symbol.
//
// The common shape of this failure is a caller who knows the symbol's name
// but guessed at its ID: "greet.go::Greet" when the real ID carries a line
// suffix, "greet.go::Greet@7" when the declaration has since moved. Both are
// recoverable without another round trip, because the symbols of the file
// have already been parsed by the time the lookup fails — so the candidates
// are in hand, and withholding them only costs the caller a call.
//
// This is the same promise the ambiguous-name path already makes: report the
// candidate IDs rather than picking one. An exact ID stays exact — nothing
// here resolves the call on the caller's behalf.
func symbolNotFoundError(symbolID string, symbols []Symbol) error {
	name, ok := symbolIDName(symbolID)
	if !ok {
		return fmt.Errorf("symbol not found: %s", symbolID)
	}

	matches := make([]string, 0, 2)
	for _, symbol := range symbols {
		if symbol.Name == name {
			matches = append(matches, symbol.ID)
		}
	}

	switch len(matches) {
	case 0:
		return fmt.Errorf("symbol not found: %s", symbolID)
	case 1:
		return fmt.Errorf("symbol not found: %s — did you mean %s?", symbolID, matches[0])
	default:
		return fmt.Errorf("symbol not found: %s — did you mean one of: %s?", symbolID, strings.Join(matches, ", "))
	}
}

// resolveSymbolInFile finds the symbol symbolID names among the file's parsed
// symbols: an exact ID match first, and failing that a unique match on the
// name the ID carries.
//
// The fallback removes a round trip that use kept paying. A caller who knows a
// declaration is called Greet has to spell "greet.go::Greet@3", and the line
// number is knowable only by asking — so every edit was preceded by an outline
// or read_symbol call whose only purpose was to learn a number the file
// already determines. Resolving a unique name is not guessing: there is
// exactly one thing it can mean, and if there is more than one, this refuses
// and names them.
//
// An exact ID still wins outright, so a caller who supplies one gets precisely
// that symbol and never a near neighbour.
func resolveSymbolInFile(symbols []Symbol, symbolID string) (Symbol, error) {
	for _, symbol := range symbols {
		if symbol.ID == symbolID {
			return symbol, nil
		}
	}

	name, ok := symbolIDName(symbolID)
	if !ok {
		return Symbol{}, symbolNotFoundError(symbolID, symbols)
	}

	matches := make([]Symbol, 0, 2)
	for _, symbol := range symbols {
		if symbol.Name == name {
			matches = append(matches, symbol)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return Symbol{}, symbolNotFoundError(symbolID, symbols)
}

// CandidatesForID returns the symbols in path whose name matches the one
// embedded in symbolID. It is the transport-facing half of
// symbolNotFoundError: read_symbol reports a failed exact lookup as a
// not_found *resolution* rather than an error, so the suggestion has to
// travel as candidates in the response rather than as error text.
//
// Returns nil when the ID is malformed or nothing matches — a not_found with
// no candidates is the honest answer to a name that is genuinely absent.
func (i *Index) CandidatesForID(path string, symbolID string) []Symbol {
	name, ok := symbolIDName(symbolID)
	if !ok {
		return nil
	}
	candidates, err := i.SymbolsByName(path, name)
	if err != nil || len(candidates) == 0 {
		return nil
	}
	return candidates
}

// NotFoundResolution builds the outline and resolution for a failed exact-ID
// lookup, carrying any same-named candidates so the caller can retry without
// a second call. Both transports share it so read_symbol cannot answer one
// way over MCP and another over the internal API.
func (i *Index) NotFoundResolution(path string, symbolID string) ([]protocol.OutlineItem, protocol.SymbolResolution) {
	resolution := protocol.SymbolResolution{
		Status: protocol.ResolutionNotFound,
		Query:  symbolID,
	}

	candidates := i.CandidatesForID(path, symbolID)
	if len(candidates) == 0 {
		return nil, resolution
	}

	outline := make([]protocol.OutlineItem, 0, len(candidates))
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.ID)
		outline = append(outline, protocol.OutlineItem{
			ID:   candidate.ID,
			Kind: candidate.Kind,
			Name: candidate.Name,
			Path: candidate.Path,
			From: candidate.From,
			To:   candidate.To,
		})
	}
	resolution.CandidateIDs = ids
	return outline, resolution
}

// symbolIDName extracts the bare declaration name from a "path::Name@line"
// symbol ID, tolerating a missing "@line" suffix because an ID without one is
// precisely the case this exists to recover from.
func symbolIDName(symbolID string) (string, bool) {
	idx := strings.LastIndex(symbolID, "::")
	if idx < 0 {
		return "", false
	}

	name := symbolID[idx+len("::"):]
	if at := strings.LastIndex(name, "@"); at >= 0 {
		name = name[:at]
	}
	if name == "" {
		return "", false
	}
	return name, true
}

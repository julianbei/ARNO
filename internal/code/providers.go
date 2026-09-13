package code

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/julianbei/jade/internal/protocol"
)

// Capability providers (release plan 0.0.6, "Providers behind a registry").
//
// A provider has an ID and serves one capability: it answers, or declines so
// the next one is asked. Core asks them strongest first, and the answer's
// provenance names the provider that gave it. References is the first
// capability on the registry, since it already had three providers wired as
// nested fallbacks; rename and diagnostics follow the same shape. Adding a
// language's semantics should mean a provider and a registration, not a
// change to the tools.

// referenceRequest is what a reference provider is asked about.
type referenceRequest struct {
	symbolID string
	symbol   Symbol
	line     int
	column   int
}

// referenceProvider answers references for one symbol, or declines.
type referenceProvider interface {
	ID() string
	references(index *Index, request referenceRequest) (protocol.ReferencesResponse, bool)
}

// referenceProviders is the references registry, strongest first. The text
// index is last and always answers, so references never comes back empty
// handed; its provenance says it is approximate.
func referenceProviders() []referenceProvider {
	return []referenceProvider{
		languageServerReferences{},
		goplsCLIReferences{},
		textIndexReferences{},
	}
}

// ReferenceProviderIDs lists the references providers, strongest first.
func ReferenceProviderIDs() []string {
	providers := referenceProviders()
	ids := make([]string, 0, len(providers))
	for _, provider := range providers {
		ids = append(ids, provider.ID())
	}
	return ids
}

// languageServerReferences asks the language's running server. It is what
// makes references exact outside Go.
type languageServerReferences struct{}

func (languageServerReferences) ID() string { return "language server" }

func (languageServerReferences) references(i *Index, request referenceRequest) (protocol.ReferencesResponse, bool) {
	refs, server, ok := i.languageServerReferences(request.symbol, request.line, request.column)
	if !ok {
		return protocol.ReferencesResponse{}, false
	}
	response := protocol.ReferencesResponse{
		Query:      request.symbolID,
		Source:     "lsp",
		References: refs,
		Summary:    fmt.Sprintf("%d references to %s (%s)", len(refs), request.symbol.Name, server),
		Provenance: protocol.Provenance{Certainty: protocol.CertaintyExact, Source: server, Completeness: protocol.CompletenessComplete},
	}
	i.noteIndexing(&response, server)
	return response, true
}

// goplsCLIReferences runs the gopls command for a Go symbol. It costs a
// process start and workspace load per call, but needs no running server, so
// it answers where the client could not start at all.
type goplsCLIReferences struct{}

func (goplsCLIReferences) ID() string { return "gopls" }

func (goplsCLIReferences) references(i *Index, request referenceRequest) (protocol.ReferencesResponse, bool) {
	if !strings.EqualFold(filepath.Ext(request.symbol.Path), ".go") {
		return protocol.ReferencesResponse{}, false
	}
	refs, ok := i.goplsReferences(request.symbol.Path, request.line, request.column)
	if !ok {
		return protocol.ReferencesResponse{}, false
	}
	return protocol.ReferencesResponse{
		Query:      request.symbolID,
		Source:     "lsp",
		References: refs,
		Summary:    fmt.Sprintf("%d references to %s (gopls)", len(refs), request.symbol.Name),
		Provenance: protocol.Provenance{Certainty: protocol.CertaintyExact, Source: "gopls", Completeness: protocol.CompletenessComplete},
	}, true
}

// textIndexReferences is the name-matched call graph: approximate, and always
// available.
type textIndexReferences struct{}

func (textIndexReferences) ID() string { return "text index" }

func (textIndexReferences) references(i *Index, request referenceRequest) (protocol.ReferencesResponse, bool) {
	refs := i.approximateReferences(request.symbolID, request.symbol)
	return protocol.ReferencesResponse{
		Query:      request.symbolID,
		Source:     "approximate",
		Provenance: protocol.Provenance{Certainty: protocol.CertaintyApproximate, Source: "text index", Completeness: protocol.CompletenessMayBeIncomplete},
		References: refs,
		Summary: fmt.Sprintf(
			"%d approximate references to %s (name-matched call graph; %s — duplicate names, dynamic dispatch and cross-file shadowing are not resolved)",
			len(refs), request.symbol.Name, i.noServerReason(request.symbol.Path)),
	}, true
}

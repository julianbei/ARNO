package internalapi

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/julianbei/arno/internal/protocol"
)

// Section caps. Context is assembled for an agent about to act, not for a
// reader browsing: a long list of marginal callers costs more than it
// informs, and Phase 10's benchmark showed exactly what unbounded responses
// do to arno's value. Every capped section reports its true total.
const (
	maxContextCallers = 10
	maxContextTests   = 10
	maxContextTypes   = 10
	maxContextBody    = 200
)

var identifierPattern = regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_]*\b`)

// Context assembles everything an agent needs to act on one symbol in a
// single call — docs/scope.md §14's North Star primitive.
//
// Every piece already existed as its own tool after Phases 1-9. The value
// here is not new capability, it is the round trips removed: an agent asking
// "I want to modify this function" otherwise issues read_symbol, references,
// a test search, and a diagnostics check, then reconciles four responses
// itself. This is the same progressive-disclosure move arno already makes
// for outline(), applied across concepts instead of within one file.
//
// Purpose selects which sections are worth their tokens. It narrows; it
// never adds anything a caller could not get another way.
func (s *Server) Context(req protocol.ContextRequest) (protocol.ContextResponse, error) {
	symbolID, err := s.resolveSymbolID(req.Path, req.SymbolID, req.SymbolName)
	if err != nil {
		return protocol.ContextResponse{}, err
	}

	purpose := normalizePurpose(req.Purpose)
	symbol, body, err := s.index.ReadSymbol(req.Path, symbolID, maxContextBody)
	if err != nil {
		return protocol.ContextResponse{}, err
	}

	response := protocol.ContextResponse{
		SymbolID:       symbolID,
		Symbol:         symbol.Name,
		Kind:           symbol.Kind,
		Path:           symbol.Path,
		Purpose:        purpose,
		Implementation: body,
	}

	if purposeWants(purpose, "callers") {
		callers, tests, provenance := s.splitReferences(req.Path, symbolID)
		response.Provenance = provenance
		response.Callers, response.CallerCount = capStrings(callers, maxContextCallers)
		if purposeWants(purpose, "tests") {
			response.Tests, response.TestCount = capStrings(tests, maxContextTests)
		}
	}

	if purposeWants(purpose, "types") {
		types := s.relatedTypes(symbol.Name, body)
		response.Types, response.TypeCount = capStrings(types, maxContextTypes)
	}

	if purposeWants(purpose, "diagnostics") {
		response.Diagnostics = s.diagnostics.Immediate(symbol.Path)
	}

	if purposeWants(purpose, "changes") {
		response.RecentChange = s.recentChangeFor(symbol.Path, symbol.Name)
	}

	response.Summary = contextSummary(response)
	return response, nil
}

// normalizePurpose maps free text onto the known purposes. An unrecognized
// purpose falls back to "modify" — the widest set — because omitting a
// section the caller needed is worse than sending one they did not.
func normalizePurpose(purpose string) string {
	switch strings.ToLower(strings.TrimSpace(purpose)) {
	case "understand", "read", "explain":
		return "understand"
	case "debug", "fix":
		return "debug"
	case "test":
		return "test"
	default:
		return "modify"
	}
}

// purposeWants says which sections each purpose earns. Reading code does not
// need diagnostics; debugging does not need the type tour; testing cares
// about existing tests above all.
func purposeWants(purpose string, section string) bool {
	sections := map[string]map[string]bool{
		"modify":     {"callers": true, "tests": true, "types": true, "diagnostics": true, "changes": true},
		"understand": {"callers": true, "types": true, "changes": true},
		"debug":      {"callers": true, "tests": true, "diagnostics": true, "changes": true},
		"test":       {"callers": true, "tests": true, "types": true},
	}
	return sections[purpose][section]
}

// splitReferences divides references into production callers and tests.
// The split is by file rather than by any new analysis: a reference from a
// _test.go file is a test exercising the symbol, and an agent about to
// change the symbol wants those two lists separately — the callers tell it
// what might break, the tests tell it what will tell it so.
func (s *Server) splitReferences(path string, symbolID string) ([]string, []string, protocol.Provenance) {
	response, err := s.index.References(path, symbolID)
	if err != nil {
		return nil, nil, protocol.Provenance{}
	}

	callerSet := make(map[string]bool)
	testSet := make(map[string]bool)
	for _, ref := range response.References {
		location := ref.Path
		if ref.Line > 0 {
			location = fmt.Sprintf("%s:%d", ref.Path, ref.Line)
		}
		if isTestFile(ref.Path) {
			testSet[location] = true
			continue
		}
		callerSet[location] = true
	}
	return sortedKeys(callerSet), sortedKeys(testSet), response.Provenance
}

func isTestFile(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	return strings.HasSuffix(base, "_test.go") ||
		strings.HasSuffix(base, ".test.ts") ||
		strings.HasSuffix(base, ".spec.ts") ||
		strings.Contains(base, "_test.")
}

// relatedTypes finds declared types the symbol's body mentions. It is a
// name-match against the repository's own declarations, so it reports
// "these types are declared here and appear in this body" — not a resolved
// type analysis, and deliberately not presented as one.
func (s *Server) relatedTypes(symbolName string, body string) []string {
	graph, err := s.index.BuildSymbolGraph()
	if err != nil {
		return nil
	}

	declaredTypes := make(map[string]string)
	for _, node := range graph.Nodes {
		switch strings.ToLower(node.Kind) {
		case "type", "struct", "interface", "class", "enum":
			declaredTypes[node.Name] = node.Path
		}
	}

	found := make(map[string]bool)
	for _, identifier := range identifierPattern.FindAllString(body, -1) {
		if identifier == symbolName {
			continue
		}
		if path, ok := declaredTypes[identifier]; ok {
			found[path+"::"+identifier] = true
		}
	}
	return sortedKeys(found)
}

// recentChangeFor reports how this one symbol changed relative to HEAD,
// reusing 9.1's delta rather than recomputing anything.
func (s *Server) recentChangeFor(path string, symbolName string) string {
	oldSource, _ := s.workspace.FileAtHead(path)
	newSource, err := os.ReadFile(filepath.Join(s.workspace.Root(), path))
	if err != nil {
		return ""
	}

	for _, change := range s.index.SymbolDelta(path, oldSource, newSource) {
		if change.Symbol == symbolName {
			return string(change.Change)
		}
	}
	return ""
}

func contextSummary(r protocol.ContextResponse) string {
	parts := []string{fmt.Sprintf("%s (%s) for %s", r.Symbol, r.Kind, r.Purpose)}
	if r.CallerCount > 0 {
		parts = append(parts, fmt.Sprintf("%d callers", r.CallerCount))
	}
	if r.TestCount > 0 {
		parts = append(parts, fmt.Sprintf("%d tests", r.TestCount))
	}
	if len(r.Diagnostics) > 0 {
		parts = append(parts, fmt.Sprintf("%d diagnostics", len(r.Diagnostics)))
	}
	if r.RecentChange != "" {
		parts = append(parts, r.RecentChange+" since HEAD")
	}
	return strings.Join(parts, " · ")
}

// capStrings truncates a list to a bound and returns the true total, so a
// cap costs detail and never accuracy.
func capStrings(values []string, max int) ([]string, int) {
	total := len(values)
	if total > max {
		return values[:max], total
	}
	return values, total
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

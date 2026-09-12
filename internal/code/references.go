package code

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/julianbei/jade/internal/protocol"
	"github.com/julianbei/jade/internal/toolchain"
)

// goplsReferencesTimeout bounds a references lookup so it can never block
// a caller indefinitely — same discipline as 4.3's gopls check timeout.
const goplsReferencesTimeout = 10 * time.Second

// goplsReferenceLine matches gopls references output: "path:line:col" or
// "path:line:startCol-endCol". Unlike gopls check's output there is no
// trailing message, so this needs its own pattern.
var goplsReferenceLine = regexp.MustCompile(`^(.+):(\d+):(\d+)(?:-\d+)?$`)

// References finds every place symbolID is referenced.
//
// It prefers gopls (compiler-resolved, exact) and falls back to the
// approximate name-matched call graph when gopls is unavailable or the
// file isn't Go — reference_droneship.md §7's prescribed migration path:
// supersede the approximate graph per language as real tooling comes
// online rather than deleting it. The response always says which source
// answered, since a name-matched edge and a compiler-verified one are not
// the same claim (§7: "the regex graph should not be presented as
// authoritative IDE-grade semantics").
func (i *Index) References(path string, symbolID string) (protocol.ReferencesResponse, error) {
	symbol, line, column, err := i.locateSymbolPosition(path, symbolID)
	if err != nil {
		return protocol.ReferencesResponse{}, err
	}

	if strings.EqualFold(filepath.Ext(symbol.Path), ".go") {
		if refs, ok := i.goplsReferences(symbol.Path, line, column); ok {
			return protocol.ReferencesResponse{
				Query:      symbolID,
				Source:     "lsp",
				References: refs,
				Summary:    fmt.Sprintf("%d references to %s (gopls)", len(refs), symbol.Name),
			}, nil
		}
	}

	refs := i.approximateReferences(symbolID, symbol)
	return protocol.ReferencesResponse{
		Query:      symbolID,
		Source:     "approximate",
		References: refs,
		Summary: fmt.Sprintf(
			"%d approximate references to %s (name-matched call graph; gopls unavailable — duplicate names, dynamic dispatch and cross-file shadowing are not resolved)",
			len(refs), symbol.Name),
	}, nil
}

// locateSymbolPosition resolves symbolID to its declaration position,
// including the 1-based column of the name within its declaring line —
// gopls addresses positions as file:line:col, but jade's Symbol only
// carries line ranges.
func (i *Index) locateSymbolPosition(path string, symbolID string) (Symbol, int, int, error) {
	absolute := i.resolvePath(path)
	rel, err := filepath.Rel(i.root, absolute)
	if err != nil {
		return Symbol{}, 0, 0, err
	}

	data, err := os.ReadFile(absolute)
	if err != nil {
		return Symbol{}, 0, 0, err
	}

	lines := splitLines(string(data))
	symbols, _ := i.symbolsForPath(absolute, rel, data, lines)
	for _, symbol := range symbols {
		if symbol.ID != symbolID {
			continue
		}
		if symbol.From < 1 || symbol.From > len(lines) {
			return Symbol{}, 0, 0, fmt.Errorf("symbol %s has invalid range", symbolID)
		}

		declaration := lines[symbol.From-1]
		offset := strings.Index(declaration, symbol.Name)
		if offset < 0 {
			// Name isn't literally on its declaring line (rare, e.g. a
			// grouped declaration): point at the line start rather than
			// failing the whole lookup.
			offset = 0
		}
		return symbol, symbol.From, offset + 1, nil
	}

	return Symbol{}, 0, 0, fmt.Errorf("symbol not found: %s", symbolID)
}

// goplsReferences shells out to `gopls references`. The bool reports
// whether gopls actually ran — false means unavailable, not "no
// references", so callers must not conflate the two.
func (i *Index) goplsReferences(relPath string, line int, column int) ([]protocol.ReferenceLocation, bool) {
	binary, ok := toolchain.Gopls()
	if !ok {
		return nil, false
	}

	ctx, cancel := context.WithTimeout(context.Background(), goplsReferencesTimeout)
	defer cancel()

	position := fmt.Sprintf("%s:%d:%d", relPath, line, column)
	cmd := exec.CommandContext(ctx, binary, "references", position)
	cmd.Dir = i.root
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return nil, false
	}
	if err != nil && len(output) == 0 {
		return nil, false
	}

	return i.parseGoplsReferences(string(output)), true
}

func (i *Index) parseGoplsReferences(output string) []protocol.ReferenceLocation {
	refs := make([]protocol.ReferenceLocation, 0)
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		match := goplsReferenceLine.FindStringSubmatch(strings.TrimSpace(scanner.Text()))
		if match == nil {
			continue
		}
		line, _ := strconv.Atoi(match[2])
		column, _ := strconv.Atoi(match[3])
		refs = append(refs, protocol.ReferenceLocation{
			Path:   i.relativePath(match[1]),
			Line:   line,
			Column: column,
		})
	}
	return refs
}

// approximateReferences answers from the name-matched call graph. Line
// numbers are unavailable from graph nodes, so they are reported as 0
// rather than guessed.
func (i *Index) approximateReferences(symbolID string, symbol Symbol) []protocol.ReferenceLocation {
	graph, err := i.BuildSymbolGraph()
	if err != nil {
		return nil
	}

	graphID := fmt.Sprintf("%s::%s", symbol.Path, symbol.Name)
	nodesByID := make(map[string]protocol.SymbolGraphNode, len(graph.Nodes))
	for _, node := range graph.Nodes {
		nodesByID[node.ID] = node
	}
	refs := make([]protocol.ReferenceLocation, 0)
	seen := make(map[string]bool)
	for _, edge := range graph.Edges {
		if edge.To != graphID || seen[edge.From] {
			continue
		}
		seen[edge.From] = true
		if node, ok := nodesByID[edge.From]; ok {
			refs = append(refs, protocol.ReferenceLocation{
				Path: node.Path,
				// The graph dedupes per calling symbol but has no line
				// numbers, so the caller's name is what makes two entries
				// in the same file tell apart.
				Symbol:     node.Name,
				Confidence: edge.Confidence,
			})
		}
	}
	return refs
}

// relativePath converts an absolute path gopls reported back into a
// workspace-relative one, matching the form every other jade response uses.
func (i *Index) relativePath(path string) string {
	if !filepath.IsAbs(path) {
		return filepath.ToSlash(path)
	}
	rel, err := filepath.Rel(i.root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

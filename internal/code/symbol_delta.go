package code

import (
	"sort"
	"strings"

	"github.com/julianbei/jade/internal/protocol"
)

// SymbolDelta reports which symbols were added, removed or modified between
// two versions of one file.
//
// scope.md §13's own MVP example has changes() naming the symbol that moved
// ("SessionManager.refreshSession modified"), not just the file. 3.2's
// numstat summary answers "how much"; this answers "what", which is the
// question an agent returning to a file actually has.
//
// Both sides are parsed with the same extractor the rest of jade uses, so a
// language jade cannot parse yields no symbol-level detail rather than a
// wrong one — the file-level counts still stand on their own.
//
// A nil or empty oldSource means the file is new: every symbol in it is
// reported as added. An empty newSource means it was deleted.
func (i *Index) SymbolDelta(relPath string, oldSource []byte, newSource []byte) []protocol.SymbolChange {
	oldSymbols := i.symbolBodies(relPath, oldSource)
	newSymbols := i.symbolBodies(relPath, newSource)

	changes := make([]protocol.SymbolChange, 0)

	for name, next := range newSymbols {
		previous, existed := oldSymbols[name]
		switch {
		case !existed:
			changes = append(changes, protocol.SymbolChange{
				Path: relPath, Symbol: name, Kind: next.kind, Change: protocol.SymbolAdded,
			})
		case previous.body != next.body:
			// Compared by body text rather than by line range: a symbol that
			// only moved because something above it grew has not changed, and
			// reporting it would make every edit look like it touched half
			// the file.
			changes = append(changes, protocol.SymbolChange{
				Path: relPath, Symbol: name, Kind: next.kind, Change: protocol.SymbolModified,
			})
		}
	}

	for name, previous := range oldSymbols {
		if _, stillThere := newSymbols[name]; !stillThere {
			changes = append(changes, protocol.SymbolChange{
				Path: relPath, Symbol: name, Kind: previous.kind, Change: protocol.SymbolRemoved,
			})
		}
	}

	sort.Slice(changes, func(a, b int) bool {
		if changes[a].Change != changes[b].Change {
			return changes[a].Change < changes[b].Change
		}
		return changes[a].Symbol < changes[b].Symbol
	})
	return changes
}

type symbolBody struct {
	kind string
	body string
}

// symbolBodies parses source held in memory. It deliberately bypasses the
// mtime/size cache in symbolsForPath: the old side of a diff never exists on
// disk, and caching it under the current file's identity would poison the
// cache with content that is not what the file contains.
func (i *Index) symbolBodies(relPath string, source []byte) map[string]symbolBody {
	bodies := make(map[string]symbolBody)
	if len(source) == 0 {
		return bodies
	}

	lines := splitLines(string(source))
	symbols, _ := i.parseSymbolsForPath(relPath, relPath, source, lines)

	for _, symbol := range symbols {
		from := symbol.From - 1
		if from < 0 {
			from = 0
		}
		to := symbol.To
		if to > len(lines) {
			to = len(lines)
		}
		if from >= to {
			continue
		}
		bodies[symbol.Name] = symbolBody{
			kind: symbol.Kind,
			body: strings.Join(lines[from:to], "\n"),
		}
	}
	return bodies
}

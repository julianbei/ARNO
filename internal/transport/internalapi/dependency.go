package internalapi

import (
	"strings"
	"sync"

	"github.com/julianbei/jade/internal/code"
	"github.com/julianbei/jade/internal/deps"
	"github.com/julianbei/jade/internal/events"
)

// DependencyPrefix marks a path inside a dependency's source rather than the
// workspace: dep:regex-syntax/src/hir/mod.rs.
const DependencyPrefix = "dep:"

type dependencyEntry struct {
	index  *code.Index
	source deps.Source
}

// dependencyIndexes caches one read-only index per workspace and dependency.
var dependencyIndexes sync.Map

// dependencyIndex returns an index rooted at a dependency's source. It is
// separate from the workspace index, and edits only ever use the workspace
// one, so nothing outside the workspace can be written through it.
func (s *Server) dependencyIndex(name string) (*code.Index, deps.Source, error) {
	root := s.workspace.Root()
	key := root + "\x00" + name
	if cached, ok := dependencyIndexes.Load(key); ok {
		entry := cached.(dependencyEntry)
		return entry.index, entry.source, nil
	}
	source, err := deps.Resolve(root, name)
	if err != nil {
		return nil, deps.Source{}, err
	}
	entry := dependencyEntry{index: code.NewIndex(source.Dir, events.NewBus()), source: source}
	dependencyIndexes.Store(key, entry)
	return entry.index, source, nil
}

// splitDependencyPath splits dep:<name>/<path>. A scoped npm name keeps both
// of its segments: dep:@scope/pkg/lib/index.js.
func splitDependencyPath(path string) (string, string, bool) {
	rest, ok := strings.CutPrefix(path, DependencyPrefix)
	if !ok {
		return "", "", false
	}
	segments := strings.SplitN(rest, "/", 3)
	if strings.HasPrefix(rest, "@") && len(segments) >= 2 {
		name := segments[0] + "/" + segments[1]
		if len(segments) == 3 {
			return name, segments[2], true
		}
		return name, "", true
	}
	name, file, _ := strings.Cut(rest, "/")
	return name, file, true
}

// indexFor picks the index a read path belongs to and the path within it.
func (s *Server) indexFor(path string) (*code.Index, string, error) {
	name, file, ok := splitDependencyPath(path)
	if !ok {
		return s.index, path, nil
	}
	index, _, err := s.dependencyIndex(name)
	if err != nil {
		return nil, "", err
	}
	return index, file, nil
}

// dependencyLabel names a dependency and the source it was read from.
func dependencyLabel(source deps.Source) string {
	label := source.Name
	if source.Version != "" {
		label += " " + source.Version
	}
	return label + " (" + source.Dir + ")"
}

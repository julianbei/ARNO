package internalapi

import (
	"errors"
	"fmt"
	gopath "path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/julianbei/jade/internal/pathguard"

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

// withDependencyHint adds, to a read refused for leaving the workspace, how
// to read the same file as a dependency. In the pilot an agent tried
// read_range on ~/.cargo/registry/src directly and was only told no.
func withDependencyHint(requested string, err error) error {
	if err == nil || !errors.Is(err, pathguard.ErrOutsideWorkspace) {
		return err
	}
	name, file := dependencyFromCachePath(requested)
	if name == "" {
		return err
	}
	return fmt.Errorf("%w; this is %s's source — read it as %s%s/%s, or search it with grep dependency: %q", err, name, DependencyPrefix, name, file, name)
}

// dependencyFromCachePath recognises a path inside a package cache and names
// the dependency and the file within it.
func dependencyFromCachePath(requested string) (string, string) {
	slashed := filepath.ToSlash(requested)
	if at := strings.Index(slashed, "/registry/src/"); at >= 0 {
		parts := strings.SplitN(slashed[at+len("/registry/src/"):], "/", 3)
		if len(parts) >= 2 {
			return trimCrateVersion(parts[1]), partAt(parts, 2)
		}
	}
	if at := strings.Index(slashed, "/pkg/mod/"); at >= 0 {
		rest := slashed[at+len("/pkg/mod/"):]
		if version := strings.Index(rest, "@"); version >= 0 {
			file := ""
			if slash := strings.Index(rest[version:], "/"); slash >= 0 {
				file = rest[version+slash+1:]
			}
			return gopath.Base(rest[:version]), file
		}
	}
	if at := strings.LastIndex(slashed, "/node_modules/"); at >= 0 {
		rest := slashed[at+len("/node_modules/"):]
		if strings.HasPrefix(rest, "@") {
			parts := strings.SplitN(rest, "/", 3)
			if len(parts) >= 2 {
				return parts[0] + "/" + parts[1], partAt(parts, 2)
			}
		}
		parts := strings.SplitN(rest, "/", 2)
		return parts[0], partAt(parts, 1)
	}
	if at := strings.Index(slashed, "/site-packages/"); at >= 0 {
		parts := strings.SplitN(slashed[at+len("/site-packages/"):], "/", 2)
		return parts[0], partAt(parts, 1)
	}
	return "", ""
}

func partAt(parts []string, index int) string {
	if index < len(parts) {
		return parts[index]
	}
	return ""
}

// trimCrateVersion drops the version from a registry directory name:
// regex-syntax-0.8.11 is regex-syntax.
func trimCrateVersion(dir string) string {
	for at := len(dir) - 1; at > 0; at-- {
		if dir[at] == '-' && at+1 < len(dir) && dir[at+1] >= '0' && dir[at+1] <= '9' {
			return dir[:at]
		}
	}
	return dir
}

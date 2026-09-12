package code

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/julianbei/jade/internal/toolchain"
)

// goplsRenameTimeout bounds a rename. A rename rewrites every call site in
// the repository, so it is the most expensive gopls operation jade runs and
// gets a longer budget than 7.1's reference lookup.
const goplsRenameTimeout = 30 * time.Second

// ErrRenameUnavailable is returned when no language server can resolve the
// rename. Unlike References (7.1), Rename deliberately has NO approximate
// fallback: BuildSymbolGraph matches on names, and applying a rename from
// name matches would silently rewrite unrelated identifiers that happen to
// share a name and miss shadowed or dynamically dispatched ones. A
// reference list that is merely approximate is still useful to a reader; an
// edit that is merely approximate is corruption. Refusing is the correct
// answer — docs/scope.md Rule 2 says to use the compiler's answer, not to guess
// when the compiler is unavailable.
var ErrRenameUnavailable = errors.New("rename requires a language server for this file's language, and none is available")

// ErrInvalidIdentifier is returned when newName could not be a valid
// identifier, caught before touching the workspace rather than after gopls
// has half-applied something.
var ErrInvalidIdentifier = errors.New("rename target is not a valid identifier")

// RenameSymbol renames symbolID to newName across the whole repository
// using `gopls rename`, the CLI surface of the LSP textDocument/rename
// action. It returns the workspace-relative paths that changed.
//
// The rename runs in two passes: `-d` produces a diff without touching
// disk, which is how the changed-file set is learned, and only then does
// `-w` apply it. That ordering also means a rename gopls would reject
// (conflicting name, unresolvable position) fails before anything is
// written.
func (i *Index) RenameSymbol(path string, symbolID string, newName string) ([]string, error) {
	if !isValidIdentifier(newName) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidIdentifier, newName)
	}

	symbol, line, column, err := i.locateSymbolPosition(path, symbolID)
	if err != nil {
		return nil, err
	}
	if symbol.Name == newName {
		return nil, fmt.Errorf("symbol %s is already named %q", symbolID, newName)
	}
	// A real language server first, which is what makes rename work outside
	// Go at all. The refusal below is still the right answer when no server
	// is available: applying a rename from name matching would silently
	// rewrite unrelated identifiers that happen to share a name.
	if changed, ok := i.languageServerRename(symbol, line, column, newName); ok {
		return changed, nil
	}

	if !strings.EqualFold(filepath.Ext(symbol.Path), ".go") {
		return nil, fmt.Errorf("%w (for %s)", ErrRenameUnavailable, symbol.Path)
	}
	if _, ok := toolchain.Gopls(); !ok {
		return nil, ErrRenameUnavailable
	}

	position := fmt.Sprintf("%s:%d:%d", symbol.Path, line, column)

	preview, err := i.runGoplsRename("-d", position, newName)
	if err != nil {
		return nil, fmt.Errorf("rename rejected by gopls: %w", err)
	}
	changed := parseRenameDiffPaths(preview, i.relativePath)
	if len(changed) == 0 {
		return nil, fmt.Errorf("gopls reported no edits for %s", symbolID)
	}

	if _, err := i.runGoplsRename("-w", position, newName); err != nil {
		return nil, fmt.Errorf("rename failed while applying: %w", err)
	}

	// The per-file symbol cache is keyed by mtime and size, so every file
	// gopls just rewrote self-invalidates on the next read. Nothing to
	// evict here.
	return changed, nil
}

func (i *Index) runGoplsRename(mode string, position string, newName string) (string, error) {
	binary, ok := toolchain.Gopls()
	if !ok {
		return "", ErrRenameUnavailable
	}

	ctx, cancel := context.WithTimeout(context.Background(), goplsRenameTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, "rename", mode, position, newName)
	cmd.Dir = i.root
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return "", fmt.Errorf("gopls rename timed out after %s", goplsRenameTimeout)
	}
	if err != nil {
		return "", fmt.Errorf("%s: %s", err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

// parseRenameDiffPaths pulls the touched file set out of gopls' unified
// diff. Only the "+++" side is read: the "---" side names the same file and
// counting both would double every entry.
func parseRenameDiffPaths(diff string, normalize func(string) string) []string {
	seen := make(map[string]bool)
	scanner := bufio.NewScanner(strings.NewReader(diff))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "+++ ") {
			continue
		}
		path := strings.TrimPrefix(line, "+++ ")
		// Unified diff headers may carry a trailing timestamp after a tab
		// and a leading "b/" prefix, depending on how the diff was produced.
		if tab := strings.IndexByte(path, '\t'); tab >= 0 {
			path = path[:tab]
		}
		path = strings.TrimSpace(path)
		path = strings.TrimPrefix(path, "b/")
		if path == "" || path == "/dev/null" {
			continue
		}
		seen[normalize(path)] = true
	}

	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

// isValidIdentifier reports whether name could be a Go identifier. gopls
// would reject a bad name anyway, but catching it here keeps the failure
// cheap and the error message specific.
func isValidIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for offset, r := range name {
		if offset == 0 {
			if !unicode.IsLetter(r) && r != '_' {
				return false
			}
			continue
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return false
		}
	}
	return !goReservedWords[name]
}

var goReservedWords = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true, "continue": true,
	"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
	"func": true, "go": true, "goto": true, "if": true, "import": true,
	"interface": true, "map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true, "var": true,
}

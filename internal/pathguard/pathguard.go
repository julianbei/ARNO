// Package pathguard keeps every file operation inside the workspace.
//
// ARNO is asked to leave running beside an agent. That claim cannot stand
// while an edit given "../../.ssh/config", an absolute path, or a symlink that
// points out of the repository writes wherever it lands. Every path a caller or
// a language server hands to ARNO resolves through Resolve before it is read or
// written, so the rule lives in one place instead of eighteen.
package pathguard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrOutsideWorkspace reports a path that resolves outside the workspace root,
// lexically or through a symlink.
var ErrOutsideWorkspace = errors.New("outside the workspace")

// Resolve returns the absolute path for path inside root, or an error naming
// the root when it escapes.
//
// Relative paths join the root; absolute paths are accepted only when they
// already lie inside it. The returned path is lexical — spelled under root as
// given, not under its symlink-resolved form — so callers computing paths
// relative to root keep getting the same answer. The symlink check runs on
// the deepest part of the path that exists, which also covers a file about to
// be created inside a symlinked directory.
func Resolve(root string, path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path is required")
	}
	root = filepath.Clean(root)
	realRoot := evalExisting(root)

	absolute := path
	if !filepath.IsAbs(absolute) {
		absolute = filepath.Join(root, absolute)
	}
	absolute = filepath.Clean(absolute)

	switch {
	case within(root, absolute):
	case within(realRoot, absolute):
		// An absolute path spelled through the real root (/private/var for
		// /var on macOS) is inside; respell it under root so relative paths
		// computed from it stay short.
		rel, _ := filepath.Rel(realRoot, absolute)
		absolute = filepath.Join(root, rel)
	default:
		return "", fmt.Errorf("%w: %s is not inside the workspace root %s", ErrOutsideWorkspace, path, root)
	}

	if real := evalExisting(absolute); !within(realRoot, real) {
		return "", fmt.Errorf("%w: %s resolves through a symlink to %s, not inside the workspace root %s",
			ErrOutsideWorkspace, path, real, root)
	}
	return absolute, nil
}

// Contains reports whether path resolves inside root. Walks use it to skip a
// symlinked file whose target lies elsewhere.
func Contains(root string, path string) bool {
	_, err := Resolve(root, path)
	return err == nil
}

// within reports whether path is root or lies beneath it, lexically.
func within(root string, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// evalExisting resolves symlinks in the longest existing prefix of path and
// appends the part that does not exist yet unchanged.
func evalExisting(path string) string {
	missing := ""
	current := path
	for {
		if _, err := os.Lstat(current); err == nil {
			real, err := filepath.EvalSymlinks(current)
			if err != nil {
				return path
			}
			return filepath.Join(real, missing)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return path
		}
		missing = filepath.Join(filepath.Base(current), missing)
		current = parent
	}
}

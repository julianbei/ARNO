package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// rootSource says where the workspace root came from, so startup can report it
// rather than leaving the operator to guess.
type rootSource string

const (
	rootFromFlag rootSource = "--root flag"
	rootFromEnv  rootSource = "JADE_WORKSPACE_ROOT"
	rootFromCwd  rootSource = "current working directory"
	envWorkspace            = "JADE_WORKSPACE_ROOT"
)

// resolvedRoot is the outcome of working out which directory jade should
// operate on.
type resolvedRoot struct {
	Path   string
	Source rootSource
	// Warning is a non-fatal problem worth telling the operator about, most
	// often that the root is not a git repository.
	Warning string
}

// resolveWorkspaceRoot decides which directory jade will inspect and edit.
//
// Precedence is --root, then JADE_WORKSPACE_ROOT, then the current working
// directory. The flag wins because it is the more specific statement: an env
// var is usually inherited from a shell or a client config and may not have
// been written with this invocation in mind.
//
// The cwd fallback stays, because it is genuinely convenient, but it is no
// longer *silent*: every path reports its source at startup. Silently
// operating on whatever directory an MCP client happened to launch from is how
// an agent ends up confidently editing the wrong repository, and that failure
// is invisible until something is already wrong.
//
// The returned path is always absolute and symlink-resolved, so paths in
// responses stay comparable regardless of how the root was spelled.
func resolveWorkspaceRoot(args []string, getenv func(string) string, getwd func() (string, error)) (resolvedRoot, error) {
	raw, source, err := rawRoot(args, getenv, getwd)
	if err != nil {
		return resolvedRoot{}, err
	}

	absolute, err := filepath.Abs(raw)
	if err != nil {
		return resolvedRoot{}, fmt.Errorf("cannot resolve workspace root %q: %w", raw, err)
	}
	// EvalSymlinks matters on macOS, where /tmp is a symlink to /private/tmp:
	// without it the root and the paths git reports disagree, and every
	// relative path jade computes comes out wrong.
	if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
		absolute = resolved
	}

	info, err := os.Stat(absolute)
	if os.IsNotExist(err) {
		return resolvedRoot{}, fmt.Errorf("workspace root does not exist: %s (from %s)", absolute, source)
	}
	if err != nil {
		return resolvedRoot{}, fmt.Errorf("cannot read workspace root %s (from %s): %w", absolute, source, err)
	}
	if !info.IsDir() {
		return resolvedRoot{}, fmt.Errorf("workspace root is not a directory: %s (from %s)", absolute, source)
	}

	out := resolvedRoot{Path: absolute, Source: source}
	if !isGitRepository(absolute) {
		// Deliberately a warning rather than a fatal error.
		//
		// Most of jade does not need git: outline, find, grep, read and every
		// edit operation work on any directory, and jade's revision tracking
		// is its own counter rather than git's. Refusing to start would make
		// jade unusable on a directory it can serve perfectly well.
		//
		// But it must be said plainly at startup, because the tools that DO
		// need git degrade in ways that look like "nothing changed" rather
		// than like an error.
		out.Warning = fmt.Sprintf(
			"%s is not a git repository — changes, diff, history and checkpoint will be limited; everything else works",
			absolute)
	}
	return out, nil
}

func rawRoot(args []string, getenv func(string) string, getwd func() (string, error)) (string, rootSource, error) {
	if flagged, found, err := rootFlag(args); err != nil {
		return "", "", err
	} else if found {
		return flagged, rootFromFlag, nil
	}

	if fromEnv := strings.TrimSpace(getenv(envWorkspace)); fromEnv != "" {
		return fromEnv, rootFromEnv, nil
	}

	cwd, err := getwd()
	if err != nil {
		return "", "", fmt.Errorf("no --root or %s given, and the working directory could not be read: %w", envWorkspace, err)
	}
	return cwd, rootFromCwd, nil
}

// rootFlag accepts --root=DIR and --root DIR, in both single- and double-dash
// spellings. An empty value is an error rather than a fall-through to the next
// source: someone who passed --root meant to choose a directory, and quietly
// using a different one would be worse than saying the value is missing.
func rootFlag(args []string) (string, bool, error) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name, value, hasValue := strings.Cut(arg, "=")
		if name != "--root" && name != "-root" {
			continue
		}

		if hasValue {
			if strings.TrimSpace(value) == "" {
				return "", false, fmt.Errorf("--root was given an empty value")
			}
			return value, true, nil
		}
		if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
			return "", false, fmt.Errorf("--root needs a directory, e.g. --root /path/to/repo")
		}
		return args[i+1], true, nil
	}
	return "", false, nil
}

// isGitRepository checks for a .git entry rather than shelling out to git,
// so the probe cannot hang or depend on git being installed. .git is a
// directory in a normal clone and a file in a worktree or submodule, and both
// count.
func isGitRepository(root string) bool {
	_, err := os.Stat(filepath.Join(root, ".git"))
	return err == nil
}

package jobs

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// TestScope selects which subset of tests RunScopedGoTests executes.
// Deliberately conservative — package-level, not call-graph-precise —
// per docs/scope.md §33's own guidance: "do not attempt sophisticated
// affected-test prediction initially."
type TestScope struct {
	// Kind is one of "all", "file", "test", or "changed".
	Kind string
	// File is the path whose containing package should be tested, for
	// Kind "file".
	File string
	// Test is the test name to run by exact match across all packages,
	// for Kind "test".
	Test string
}

// RunScopedGoTests runs go test scoped per the TestScope, using
// changedFiles (typically from workspace.Manager.Changes()) for Kind
// "changed". Falls through the same async job/RunCommand machinery as
// RunValidationCommand.
func (r *Runner) RunScopedGoTests(id string, dir string, scope TestScope, changedFiles []string) {
	args, err := goTestArgsForScope(dir, scope, changedFiles)
	if err != nil {
		r.CompleteWithOutput(id, err.Error())
		return
	}
	if args == nil {
		r.CompleteWithOutput(id, "no changed Go packages to test")
		return
	}
	r.RunCommand(id, dir, "go", args...)
}

func goTestArgsForScope(root string, scope TestScope, changedFiles []string) ([]string, error) {
	switch scope.Kind {
	case "", "all":
		return []string{"test", "./..."}, nil
	case "test":
		if scope.Test == "" {
			return nil, fmt.Errorf("test scope requires a test name")
		}
		return []string{"test", "-run", "^" + regexp.QuoteMeta(scope.Test) + "$", "./..."}, nil
	case "file":
		if scope.File == "" {
			return nil, fmt.Errorf("file scope requires a file path")
		}
		pkg, err := packageArgForFile(root, scope.File)
		if err != nil {
			return nil, err
		}
		return []string{"test", pkg}, nil
	case "changed":
		pkgs := uniquePackageArgs(root, changedFiles)
		if len(pkgs) == 0 {
			return nil, nil
		}
		return append([]string{"test"}, pkgs...), nil
	default:
		return nil, fmt.Errorf("unknown test scope: %q", scope.Kind)
	}
}

// packageArgForFile converts a file path into a "./relative/dir" package
// argument go test accepts.
func packageArgForFile(root string, file string) (string, error) {
	absolute := file
	if !filepath.IsAbs(absolute) {
		absolute = filepath.Join(root, file)
	}
	dir := filepath.Dir(absolute)
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || rel == "" {
		return ".", nil
	}
	return "./" + rel, nil
}

// uniquePackageArgs maps changed files to their containing packages,
// de-duplicated and sorted, skipping non-Go files entirely.
func uniquePackageArgs(root string, files []string) []string {
	seen := make(map[string]bool, len(files))
	pkgs := make([]string, 0, len(files))
	for _, file := range files {
		if !strings.HasSuffix(file, ".go") {
			continue
		}
		pkg, err := packageArgForFile(root, file)
		if err != nil || seen[pkg] {
			continue
		}
		seen[pkg] = true
		pkgs = append(pkgs, pkg)
	}
	sort.Strings(pkgs)
	return pkgs
}

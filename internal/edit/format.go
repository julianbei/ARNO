package edit

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const formatTimeout = 20 * time.Second

// formatTouched formats files after a single edit, the same way Apply does
// for a batch.
//
// Every edit path runs this, because formatting drift was invisible to
// everything else jade reports: `go build`, `go vet` and `go test` all pass
// on badly formatted code, so jade said "success" at every step while a
// repository's formatting degraded through accumulated edits. Four files in
// this repo had drifted out of gofmt shape before anyone noticed.
//
// It runs before diagnostics are computed, so reported line numbers match
// the file as it finally stands rather than the pre-format version.
func (s *Service) formatTouched(paths ...string) []string {
	return formatFiles(s.workspace.Root(), paths)
}

// formatFiles runs the language formatter over touched files and returns the
// ones that were actually reformatted.
//
// A missing formatter is not an error. The edits themselves succeeded, and
// failing the whole batch because `gofmt` is absent would be worse than
// leaving the file unformatted.
func formatFiles(root string, paths []string) []string {
	formatted := make([]string, 0, len(paths))
	for _, path := range paths {
		chosen, ok := formatterFor(root, path)
		if !ok {
			continue
		}
		if runFormatter(root, chosen, path) {
			formatted = append(formatted, path)
		}
	}
	return formatted
}

// formatter is one resolved formatter invocation for one file.
type formatter struct {
	name   string
	binary string
	// args are placed before the file path.
	args []string
}

// formatterFor picks the formatter for a file, or none.
//
// Two tiers, and the difference between them is the whole design.
//
// gofmt and rustfmt are canonical: one output for a given input, no project
// configuration, universally expected by their ecosystems. They run whenever
// they are installed.
//
// Every other formatter takes project configuration, and running one a
// project did not choose could reformat far more than the agent touched — a
// one-line edit arriving as a 400-line diff. So those run only when the
// repository itself declares the formatter: its config file, and for
// Node and Python the project's own installed binary, so the version that
// runs is the version the project pinned. A repository that declares nothing
// gets its files back exactly as edited.
//
// Ruby is deliberately absent. rubocop's autocorrect applies lint fixes, not
// just layout, and a formatter that changes behaviour is not a formatter.
func formatterFor(root string, path string) (formatter, bool) {
	dir := filepath.Dir(filepath.Join(root, path))

	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		if binary, ok := lookPathFormatter("gofmt"); ok {
			return formatter{name: "gofmt", binary: binary, args: []string{"-w"}}, true
		}

	case ".rs":
		if binary, ok := lookPathFormatter("rustfmt"); ok {
			return formatter{name: "rustfmt", binary: binary}, true
		}

	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
		if !prettierConfigured(root, dir) {
			return formatter{}, false
		}
		if binary, ok := findUpExecutable(root, dir, filepath.Join("node_modules", ".bin", "prettier")); ok {
			return formatter{name: "prettier", binary: binary, args: []string{"--write", "--log-level", "warn"}}, true
		}

	case ".py":
		tool, ok := pythonFormatterConfigured(root, dir)
		if !ok {
			return formatter{}, false
		}
		binary, found := findUpExecutable(root, dir,
			filepath.Join(".venv", "bin", tool),
			filepath.Join("venv", "bin", tool))
		if !found {
			binary, found = lookPathFormatter(tool)
		}
		if !found {
			return formatter{}, false
		}
		if tool == "ruff" {
			return formatter{name: "ruff", binary: binary, args: []string{"format", "--quiet"}}, true
		}
		return formatter{name: "black", binary: binary, args: []string{"--quiet"}}, true

	case ".scala", ".sc":
		if _, ok := findUp(root, dir, ".scalafmt.conf"); !ok {
			return formatter{}, false
		}
		if binary, ok := lookPathFormatter("scalafmt"); ok {
			return formatter{name: "scalafmt", binary: binary, args: []string{"--quiet"}}, true
		}
	}
	return formatter{}, false
}

var prettierConfigFiles = []string{
	".prettierrc", ".prettierrc.json", ".prettierrc.yaml", ".prettierrc.yml",
	".prettierrc.json5", ".prettierrc.js", ".prettierrc.cjs", ".prettierrc.mjs",
	".prettierrc.toml", "prettier.config.js", "prettier.config.cjs", "prettier.config.mjs",
}

// prettierConfigured reports whether the nearest project declares prettier,
// either with a config file or a "prettier" key in package.json.
func prettierConfigured(root string, dir string) bool {
	if _, ok := findUp(root, dir, prettierConfigFiles...); ok {
		return true
	}
	manifest, ok := findUp(root, dir, "package.json")
	if !ok {
		return false
	}
	data, err := os.ReadFile(manifest)
	return err == nil && bytes.Contains(data, []byte(`"prettier"`))
}

// pythonFormatterConfigured reads the nearest pyproject.toml for a declared
// formatter. ruff is checked for its format section specifically: a project
// using ruff only as a linter has not chosen ruff's formatting.
func pythonFormatterConfigured(root string, dir string) (string, bool) {
	manifest, ok := findUp(root, dir, "pyproject.toml")
	if !ok {
		return "", false
	}
	data, err := os.ReadFile(manifest)
	if err != nil {
		return "", false
	}
	text := string(data)
	switch {
	case strings.Contains(text, "[tool.ruff.format]"):
		return "ruff", true
	case strings.Contains(text, "[tool.black]"):
		return "black", true
	}
	return "", false
}

// findUp looks for any of names in dir and each parent up to and including
// root, returning the first path found. Stopping at root keeps a repository
// from inheriting a formatter declared by whatever directory it sits in.
func findUp(root string, dir string, names ...string) (string, bool) {
	root = filepath.Clean(root)
	for current := filepath.Clean(dir); ; current = filepath.Dir(current) {
		for _, name := range names {
			candidate := filepath.Join(current, name)
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate, true
			}
		}
		if current == root || !strings.HasPrefix(current, root) || current == filepath.Dir(current) {
			return "", false
		}
	}
}

// findUpExecutable is findUp restricted to executables.
func findUpExecutable(root string, dir string, names ...string) (string, bool) {
	root = filepath.Clean(root)
	for current := filepath.Clean(dir); ; current = filepath.Dir(current) {
		for _, name := range names {
			candidate := filepath.Join(current, name)
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
				return candidate, true
			}
		}
		if current == root || !strings.HasPrefix(current, root) || current == filepath.Dir(current) {
			return "", false
		}
	}
}

func lookPathFormatter(name string) (string, bool) {
	if path, err := exec.LookPath(name); err == nil {
		return path, true
	}
	return "", false
}

// runFormatter rewrites one file in place, reporting whether the content
// actually changed so the response names only files that really moved.
func runFormatter(root string, chosen formatter, path string) bool {
	absolute := filepath.Join(root, path)

	before, err := os.ReadFile(absolute)
	if err != nil {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), formatTimeout)
	defer cancel()

	args := append(append([]string{}, chosen.args...), absolute)
	cmd := exec.CommandContext(ctx, chosen.binary, args...)
	cmd.Dir = root
	if err := cmd.Run(); err != nil {
		// A formatter that refuses (syntax error mid-batch, say) leaves the
		// file as the edits left it. The edits themselves still stand.
		return false
	}

	after, err := os.ReadFile(absolute)
	if err != nil {
		return false
	}
	return string(before) != string(after)
}

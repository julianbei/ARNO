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
// everything else ARNO reports: `go build`, `go vet` and `go test` all pass
// on badly formatted code, so ARNO said "success" at every step while a
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
	// viaStdin formats the file's content from standard input and writes the
	// result back itself, instead of handing the formatter the path. Needed
	// where the path form does more than the file asked for: rustfmt given
	// src/lib.rs also rewrites every file that lib.rs declares with `mod x;`.
	viaStdin bool
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
			return rustfmtFor(root, dir, binary), true
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

// rustfmtFor builds the rustfmt invocation for a file in dir.
//
// Two things were wrong with running `rustfmt <file>`, and both broke the
// promise that an edit leaves a project's own formatting rules in charge:
//
// It formats the whole module tree below the file. Editing src/lib.rs or
// main.rs or any mod.rs rewrote every file they declare — a fleet session
// reported fourteen files it never touched reformatted after an edit to two.
// Standard input formats exactly the content it is given and nothing else.
//
// It assumes edition 2015 unless told otherwise, because `cargo fmt` is what
// reads the edition from Cargo.toml, not rustfmt. Async code is a syntax error
// there, so rustfmt refused and the file was silently never formatted. The
// edition comes from the project, and not at all when the project's
// rustfmt.toml sets one itself, since a flag would override the config it
// declared.
//
// rustfmt still finds rustfmt.toml and .rustfmt.toml by searching up from its
// working directory when reading standard input, so the command runs in the
// file's directory and a nested config nearer the file wins, as it does for
// cargo fmt.
func rustfmtFor(root string, dir string, binary string) formatter {
	args := []string{"--emit", "stdout"}
	if edition := rustEdition(root, dir); edition != "" {
		args = append([]string{"--edition", edition}, args...)
	}
	return formatter{name: "rustfmt", binary: binary, args: args, viaStdin: true}
}

// defaultRustEdition is used when neither rustfmt.toml nor any Cargo.toml says
// which edition the project is in. rustfmt's own default, 2015, predates async
// and would refuse most Rust written this decade.
const defaultRustEdition = "2021"

// rustEdition resolves the edition to format a file in dir with. It returns
// "" when the project's rustfmt config sets one, which must then be left
// alone.
func rustEdition(root string, dir string) string {
	if config, ok := findUp(root, dir, "rustfmt.toml", ".rustfmt.toml"); ok {
		if data, err := os.ReadFile(config); err == nil && tomlSetsKey(string(data), "edition") {
			return ""
		}
	}

	manifest, ok := findUp(root, dir, "Cargo.toml")
	if !ok {
		return defaultRustEdition
	}
	data, err := os.ReadFile(manifest)
	if err != nil {
		return defaultRustEdition
	}
	text := string(data)
	if edition := tomlStringValue(text, "edition"); edition != "" {
		return edition
	}
	// edition.workspace = true: the workspace root's [workspace.package] has it.
	if tomlSetsKey(text, "edition.workspace") {
		if workspaceManifest, ok := findUp(root, filepath.Dir(filepath.Dir(manifest)), "Cargo.toml"); ok {
			if wdata, err := os.ReadFile(workspaceManifest); err == nil {
				if edition := tomlStringValue(string(wdata), "edition"); edition != "" {
					return edition
				}
			}
		}
	}
	return defaultRustEdition
}

// tomlSetsKey reports whether text assigns key at the start of a line. It is a
// line scan, not a TOML parser: the two files it reads here are small, flat in
// the part that matters, and a wrong answer only falls back to the default.
func tomlSetsKey(text string, key string) bool {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, key) {
			rest := strings.TrimSpace(trimmed[len(key):])
			if strings.HasPrefix(rest, "=") {
				return true
			}
		}
	}
	return false
}

// tomlStringValue returns the quoted value of the first `key = "value"` line.
func tomlStringValue(text string, key string) string {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, key) {
			continue
		}
		rest := strings.TrimSpace(trimmed[len(key):])
		if !strings.HasPrefix(rest, "=") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(rest, "="))
		if hash := strings.Index(value, "#"); hash >= 0 {
			value = strings.TrimSpace(value[:hash])
		}
		value = strings.Trim(value, "\"'")
		if value != "" && !strings.ContainsAny(value, " {") {
			return value
		}
	}
	return ""
}

// FormatterName names the formatter an edit to path would run, if any.
func FormatterName(root string, path string) (string, bool) {
	chosen, ok := formatterFor(root, path)
	return chosen.name, ok
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
	// ruff reads its own config file before pyproject.toml, so a project that
	// keeps its formatter settings in ruff.toml has chosen ruff just as one
	// with [tool.ruff.format] has.
	if config, ok := findUp(root, dir, "ruff.toml", ".ruff.toml"); ok {
		if data, err := os.ReadFile(config); err == nil && strings.Contains(string(data), "[format]") {
			return "ruff", true
		}
	}

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

// runStdinFormatter formats a file by feeding its content to the formatter on
// standard input and writing the result back, for formatters whose path form
// reaches past the file. The command runs in the file's directory so a
// formatter that searches upward for its config finds the nearest one.
//
// A formatter that fails, or prints nothing for a non-empty file, leaves the
// file as the edits left it: writing back an empty result would turn a
// formatter hiccup into a deleted file.
func runStdinFormatter(ctx context.Context, chosen formatter, absolute string, before []byte) bool {
	cmd := exec.CommandContext(ctx, chosen.binary, chosen.args...)
	cmd.Dir = filepath.Dir(absolute)
	cmd.Stdin = bytes.NewReader(before)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return false
	}
	if out.Len() == 0 && len(before) > 0 {
		return false
	}
	if bytes.Equal(before, out.Bytes()) {
		return false
	}
	return writeFile(absolute, out.Bytes()) == nil
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

	if chosen.viaStdin {
		return runStdinFormatter(ctx, chosen, absolute, before)
	}

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

// Package project reads .arno/project.json: a repository's own statement of
// how ARNO should build, check and test it, committed next to the code the
// way an editor keeps its settings in .vscode/.
//
// Discovery guesses from manifests, and the pilot benchmark showed where
// guessing goes wrong: a Makefile's `python -m pytest` ran the system
// interpreter, `npm run test` ran lint, build and browser suites for a one-file
// check, and a Go module with Node packages beside it could not say what
// "build" meant. A declared config answers those once. Empty fields fall back
// to discovery, so a config can state only what discovery gets wrong.
package project

import (
	"github.com/julianbei/arno/internal/compat"

	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Dir and File locate the config inside the workspace, beside commands.json.
const (
	Dir  = ".arno"
	File = "project.json"
)

// Source names the config in responses that say where a command came from.
const Source = ".arno/project.json"

// RelPath is the config's path relative to the workspace root.
var RelPath = filepath.Join(Dir, File)

// Area is one part of the repository with its own language and commands. Its
// commands run inside its path.
type Area struct {
	Path      string `json:"path"`
	Language  string `json:"language,omitempty"`
	Build     string `json:"build,omitempty"`
	Typecheck string `json:"typecheck,omitempty"`
	Test      string `json:"test,omitempty"`
	// TestFile runs one test file; {file} is the file relative to the area.
	TestFile string `json:"testFile,omitempty"`
	// TestName runs tests by name; {name} is the name, {file} optionally the
	// file it is in.
	TestName string `json:"testName,omitempty"`
}

// Env is the environment commands run in.
type Env struct {
	// Python is the interpreter commands should find first on PATH, e.g.
	// .venv/bin/python.
	Python string            `json:"python,omitempty"`
	Vars   map[string]string `json:"vars,omitempty"`
}

// Config is a parsed .arno/project.json.
type Config struct {
	Areas []Area `json:"areas"`
	Env   Env    `json:"env,omitzero"`
	// Generated lists paths searches skip: build output, generated code.
	Generated []string `json:"generated,omitempty"`
	// Notes is short guidance for the agent, sent when a session starts.
	Notes string `json:"notes,omitempty"`
}

// Load reads the config for the workspace at root. A missing file is not an
// error and returns nil; a file that does not parse, names an unknown field or
// a path outside the workspace is, because acting on half a config would run
// commands nobody declared.
func Load(root string) (*Config, error) {
	data, err := os.ReadFile(compat.StatePath(root, File))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var config Config
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("%s is not valid: %w", Source, err)
	}
	for i, area := range config.Areas {
		clean, err := cleanPath(area.Path)
		if err != nil {
			return nil, fmt.Errorf("%s: area %d: %w", Source, i+1, err)
		}
		config.Areas[i].Path = clean
	}
	for i, entry := range config.Generated {
		clean, err := cleanPath(entry)
		if err != nil {
			return nil, fmt.Errorf("%s: generated: %w", Source, err)
		}
		config.Generated[i] = clean
	}
	return &config, nil
}

func cleanPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return ".", nil
	}
	if filepath.IsAbs(p) {
		return "", fmt.Errorf("path %q must be relative to the workspace root", p)
	}
	clean := path.Clean(filepath.ToSlash(p))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("path %q leaves the workspace", p)
	}
	return clean, nil
}

func (a Area) command(kind string) string {
	switch kind {
	case "build":
		return a.Build
	case "typecheck":
		return a.Typecheck
	case "tests":
		return a.Test
	}
	return ""
}

func (a Area) relative(file string) string {
	file = path.Clean(filepath.ToSlash(file))
	if a.Path == "." {
		return file
	}
	return strings.TrimPrefix(file, a.Path+"/")
}

// Command is the shell command for a check kind (build, typecheck, tests)
// across every area that declares one, each run inside its area.
func (c *Config) Command(kind string) (string, bool) {
	if c == nil {
		return "", false
	}
	parts := []string{}
	for _, area := range c.Areas {
		if command := strings.TrimSpace(area.command(kind)); command != "" {
			parts = append(parts, inArea(area.Path, command))
		}
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, " && "), true
}

// AreaFor is the area a workspace-relative file belongs to: the deepest area
// whose path contains it.
func (c *Config) AreaFor(file string) (Area, bool) {
	if c == nil {
		return Area{}, false
	}
	file = path.Clean(filepath.ToSlash(file))
	best, bestDepth := -1, -1
	for i, area := range c.Areas {
		depth := 0
		switch {
		case area.Path == ".":
		case file == area.Path || strings.HasPrefix(file, area.Path+"/"):
			depth = len(area.Path)
		default:
			continue
		}
		if depth > bestDepth {
			best, bestDepth = i, depth
		}
	}
	if best < 0 {
		return Area{}, false
	}
	return c.Areas[best], true
}

// TestFileCommand runs one test file with its area's testFile command.
func (c *Config) TestFileCommand(file string) (string, bool) {
	if c == nil || strings.TrimSpace(file) == "" {
		return "", false
	}
	area, ok := c.AreaFor(file)
	if !ok || area.TestFile == "" {
		return "", false
	}
	return inArea(area.Path, fill(area.TestFile, area.relative(file), "")), true
}

// TestNameCommand runs tests by name with the testName command of the file's
// area, or of the root area when no file is given.
func (c *Config) TestNameCommand(file string, name string) (string, bool) {
	if c == nil || strings.TrimSpace(name) == "" {
		return "", false
	}
	var area Area
	ok := false
	if strings.TrimSpace(file) != "" {
		area, ok = c.AreaFor(file)
	} else {
		for _, candidate := range c.Areas {
			if candidate.TestName != "" && (!ok || candidate.Path == ".") {
				area, ok = candidate, true
			}
		}
	}
	if !ok || area.TestName == "" {
		return "", false
	}
	relative := ""
	if strings.TrimSpace(file) != "" {
		relative = area.relative(file)
	}
	return inArea(area.Path, fill(area.TestName, relative, name)), true
}

// IsGenerated reports a workspace-relative path the config marks generated:
// the path itself, anything under it, or a match of a glob entry.
func (c *Config) IsGenerated(rel string) bool {
	if c == nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	for _, entry := range c.Generated {
		if entry == "." {
			continue
		}
		if rel == entry || strings.HasPrefix(rel, entry+"/") {
			return true
		}
		if matched, _ := path.Match(entry, rel); matched {
			return true
		}
		if !strings.Contains(entry, "/") {
			if matched, _ := path.Match(entry, path.Base(rel)); matched {
				return true
			}
		}
	}
	return false
}

// PythonBinDir is the directory of the configured interpreter, for PATH.
func (c *Config) PythonBinDir(root string) string {
	if c == nil || strings.TrimSpace(c.Env.Python) == "" {
		return ""
	}
	interpreter := c.Env.Python
	if !filepath.IsAbs(interpreter) {
		interpreter = filepath.Join(root, interpreter)
	}
	return filepath.Dir(interpreter)
}

// Summary lists the areas for a session's opening instructions.
func (c *Config) Summary() string {
	if c == nil || len(c.Areas) == 0 {
		return "no areas"
	}
	parts := make([]string, 0, len(c.Areas))
	for _, area := range c.Areas {
		label := area.Path
		if area.Language != "" {
			label += " (" + area.Language + ")"
		}
		parts = append(parts, label)
	}
	return "areas " + strings.Join(parts, ", ")
}

// Quote quotes s for a POSIX shell.
func Quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func inArea(areaPath string, command string) string {
	if areaPath == "." {
		return command
	}
	return "(cd " + Quote(areaPath) + " && " + command + ")"
}

// fill substitutes {file} and {name}, quoted. `*{name}*`, a runner's glob
// around a name, is quoted as one word so the shell passes the stars through.
func fill(template string, file string, name string) string {
	out := strings.ReplaceAll(template, "*{name}*", Quote("*"+name+"*"))
	out = strings.ReplaceAll(out, "{name}", Quote(name))
	if file == "" {
		out = strings.ReplaceAll(out, "{file}", "")
	} else {
		out = strings.ReplaceAll(out, "{file}", Quote(file))
	}
	for strings.Contains(out, "  ") {
		out = strings.ReplaceAll(out, "  ", " ")
	}
	return strings.TrimSpace(out)
}

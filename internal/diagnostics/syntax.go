package diagnostics

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/julianbei/jade/internal/protocol"
)

// syntaxCheckers maps config-file extensions to built-in parsers.
//
// JSON and YAML have no language server in most setups and do not need one:
// the question after an edit is only "does this still parse", and the
// standard parsers answer it in microseconds. A broken package.json or CI
// workflow is otherwise discovered by whatever reads it next — a build, a
// deploy, a CI run — long after the edit that broke it.
var syntaxCheckers = map[string]func(path string, data []byte) []protocol.Diagnostic{
	".json": checkJSON,
	".yaml": checkYAML,
	".yml":  checkYAML,
}

// checkSyntax runs the built-in parser for path's extension, if there is one.
func checkSyntax(path string, absolute string, ext string) (Result, bool) {
	checker, ok := syntaxCheckers[strings.ToLower(ext)]
	if !ok {
		return Result{}, false
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		return Result{}, false
	}
	name := "json"
	if strings.ToLower(ext) != ".json" {
		name = "yaml"
	}
	return Result{Diagnostics: checker(path, data), Checker: name}, true
}

// checkJSON reports the first syntax error with its line and column.
//
// Every value in the file is decoded, not just the first: `{} {` is a
// broken file whose first value is fine. Comments and trailing commas are
// errors, as they are to every strict JSON reader — tsconfig.json-style JSONC
// is the one common exception, and is recognised by name.
func checkJSON(path string, data []byte) []protocol.Diagnostic {
	if isJSONC(path) {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	for {
		var value any
		err := decoder.Decode(&value)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err == nil {
			continue
		}

		offset := decoder.InputOffset()
		var syntaxErr *json.SyntaxError
		if errors.As(err, &syntaxErr) {
			offset = syntaxErr.Offset
		}
		line, column := lineColumn(data, offset)
		return []protocol.Diagnostic{{
			Level:   protocol.DiagnosticError,
			Path:    path,
			Line:    line,
			Column:  column,
			Message: "invalid JSON: " + err.Error(),
		}}
	}
}

// isJSONC names files that are conventionally JSON with comments, where a
// strict parse would report errors the file's actual reader accepts.
func isJSONC(path string) bool {
	base := strings.ToLower(path[strings.LastIndex(path, "/")+1:])
	switch {
	case strings.HasPrefix(base, "tsconfig") && strings.HasSuffix(base, ".json"),
		strings.HasPrefix(base, "jsconfig") && strings.HasSuffix(base, ".json"),
		base == "devcontainer.json", base == ".devcontainer.json",
		strings.HasSuffix(path, ".vscode/settings.json"),
		strings.HasSuffix(path, ".vscode/launch.json"),
		strings.HasSuffix(path, ".vscode/tasks.json"):
		return true
	}
	return false
}

var yamlLine = regexp.MustCompile(`line (\d+)`)

// checkYAML parses every document in the file and reports the first error.
func checkYAML(path string, data []byte) []protocol.Diagnostic {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var node yaml.Node
		err := decoder.Decode(&node)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err == nil {
			continue
		}

		line := 0
		message := strings.TrimPrefix(err.Error(), "yaml: ")
		if match := yamlLine.FindStringSubmatch(message); match != nil {
			line, _ = strconv.Atoi(match[1])
			// The line is already in the location; repeating it in the
			// message is noise on every YAML error.
			message = strings.TrimPrefix(message, match[0]+": ")
		}
		return []protocol.Diagnostic{{
			Level:   protocol.DiagnosticError,
			Path:    path,
			Line:    line,
			Message: "invalid YAML: " + message,
		}}
	}
}

// lineColumn converts a byte offset into a 1-based line and column.
func lineColumn(data []byte, offset int64) (int, int) {
	if offset > int64(len(data)) {
		offset = int64(len(data))
	}
	line, column := 1, 1
	for _, b := range data[:offset] {
		if b == '\n' {
			line++
			column = 1
			continue
		}
		column++
	}
	return line, column
}

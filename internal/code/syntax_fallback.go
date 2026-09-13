package code

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	tsjava "github.com/smacker/go-tree-sitter/java"
	tsjavascript "github.com/smacker/go-tree-sitter/javascript"
	tspython "github.com/smacker/go-tree-sitter/python"
	tsruby "github.com/smacker/go-tree-sitter/ruby"
	tsrust "github.com/smacker/go-tree-sitter/rust"
	tsscala "github.com/smacker/go-tree-sitter/scala"
	tstsx "github.com/smacker/go-tree-sitter/typescript/tsx"
	tstypescript "github.com/smacker/go-tree-sitter/typescript/typescript"

	"github.com/julianbei/jade/internal/protocol"
)

// maxSyntaxDiagnostics bounds how many syntax errors one file reports. A
// single missing brace can make a parser flag every line after it.
const maxSyntaxDiagnostics = 5

// syntaxGrammar is the tree-sitter grammar for path's extension, nil when Jade
// has none.
func syntaxGrammar(path string) *sitter.Language {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ts", ".mts", ".cts":
		return tstypescript.GetLanguage()
	case ".tsx":
		return tstsx.GetLanguage()
	case ".js", ".jsx", ".mjs", ".cjs":
		return tsjavascript.GetLanguage()
	case ".py":
		return tspython.GetLanguage()
	case ".rs":
		return tsrust.GetLanguage()
	case ".rb":
		return tsruby.GetLanguage()
	case ".java":
		return tsjava.GetLanguage()
	case ".scala", ".sc":
		return tsscala.GetLanguage()
	}
	return nil
}

// syntaxCheckerName names the fallback in an edit response, with the reason
// the language server did not check the file.
func syntaxCheckerName(reason string) string {
	return fmt.Sprintf("tree-sitter (syntax only; %s)", reason)
}

// syntaxDiagnostics parses a file with its tree-sitter grammar and reports the
// syntax errors, for a language whose server is not installed or not running.
//
// In the pilot benchmark no language server ran for TypeScript, Python or
// Rust, so every edit there said "not checked" and a broken edit surfaced only
// at the next test run. A parse catches the unbalanced brace or the stray
// token in milliseconds; type errors still need the server.
func (i *Index) syntaxDiagnostics(path string) ([]protocol.Diagnostic, bool) {
	grammar := syntaxGrammar(path)
	if grammar == nil {
		return nil, false
	}
	absolute, err := i.resolvePath(path)
	if err != nil {
		return nil, false
	}
	source, err := os.ReadFile(absolute)
	if err != nil {
		return nil, false
	}
	parser := sitter.NewParser()
	parser.SetLanguage(grammar)
	tree, err := parser.ParseCtx(context.Background(), nil, source)
	if err != nil || tree == nil {
		return nil, false
	}
	defer tree.Close()

	diagnostics := []protocol.Diagnostic{}
	collectSyntaxErrors(tree.RootNode(), source, path, &diagnostics)
	return diagnostics, true
}

func collectSyntaxErrors(node *sitter.Node, source []byte, path string, diagnostics *[]protocol.Diagnostic) {
	if node == nil || len(*diagnostics) >= maxSyntaxDiagnostics || !node.HasError() {
		return
	}
	if node.IsError() || node.IsMissing() {
		message := "syntax error"
		if node.IsMissing() {
			message = "missing " + node.Type()
		} else if near := firstLine(node.Content(source)); near != "" {
			message = fmt.Sprintf("syntax error near `%s`", near)
		}
		point := node.StartPoint()
		*diagnostics = append(*diagnostics, protocol.Diagnostic{
			Level:   protocol.DiagnosticError,
			Path:    path,
			Line:    int(point.Row) + 1,
			Column:  int(point.Column) + 1,
			Message: message,
		})
		if node.IsError() {
			return
		}
	}
	for index := 0; index < int(node.ChildCount()); index++ {
		collectSyntaxErrors(node.Child(index), source, path, diagnostics)
	}
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	if len(line) > 40 {
		line = line[:40] + "…"
	}
	return line
}

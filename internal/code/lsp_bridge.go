package code

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/julianbei/jade/internal/lsp"
	"github.com/julianbei/jade/internal/protocol"
)

// This file is the only place internal/code talks to a language server.
// Keeping it in one file means the answer to "what happens when no server is
// available" is readable in one sitting: every function here returns a bool,
// and false always means "use the existing fallback", never "fail the call".

// languageServerReferences asks a real language server for usages.
//
// The returned string names the server, because a caller reading "42
// references" deserves to know whether a compiler said so or a text match did.
func (i *Index) languageServerReferences(symbol Symbol, line int, column int) ([]protocol.ReferenceLocation, string, bool) {
	ctx := context.Background()
	absolute := i.resolvePath(symbol.Path)

	client, language, ok := i.languageClient(ctx, symbol.Path)
	if !ok {
		return nil, "", false
	}

	lineText := i.lineAt(absolute, line)
	locations, ok := lsp.References(ctx, client, absolute, lineText, line, column)
	if !ok {
		return nil, language, false
	}

	// No references is a real answer — a private helper used once, an unused
	// export — but it is indistinguishable here from a server that indexed
	// nothing. The approximate graph is a better answer than an empty list in
	// the second case and no worse in the first, so fall through.
	if len(locations) == 0 {
		return nil, language, false
	}

	refs := make([]protocol.ReferenceLocation, 0, len(locations))
	for _, location := range locations {
		path := i.relativePath(lsp.URIPath(location.URI))
		refs = append(refs, protocol.ReferenceLocation{
			Path:   path,
			Line:   location.Range.Start.Line + 1,
			Column: location.Range.Start.Character + 1,
		})
	}
	return refs, language, true
}

// languageServerRename asks a real language server to rename, returning the
// workspace-relative paths it changed.
//
// The edits are applied here rather than by the server because jade owns the
// files: a server that wrote them itself would produce a change with no
// revision bump, no formatting and no diagnostics, which is precisely the
// invisible edit jade exists to prevent.
func (i *Index) languageServerRename(symbol Symbol, line int, column int, newName string) ([]string, bool) {
	ctx := context.Background()
	absolute := i.resolvePath(symbol.Path)

	client, _, ok := i.languageClient(ctx, symbol.Path)
	if !ok {
		return nil, false
	}

	lineText := i.lineAt(absolute, line)
	fileEdits, ok := lsp.Rename(ctx, client, absolute, lineText, line, column, newName)
	if !ok || len(fileEdits) == 0 {
		return nil, false
	}

	// Preflight every file before writing any of them. A rename that succeeds
	// in three files and fails in the fourth leaves the workspace in a state
	// that compiles nowhere and that nobody asked for.
	contents := make(map[string][]string, len(fileEdits))
	for _, fileEdit := range fileEdits {
		data, err := os.ReadFile(fileEdit.Path)
		if err != nil {
			return nil, false
		}
		contents[fileEdit.Path] = splitLines(string(data))
	}

	for _, fileEdit := range fileEdits {
		lines := contents[fileEdit.Path]
		for _, edit := range fileEdit.Edits {
			updated, ok := applyPositionedEdit(lines, edit)
			if !ok {
				return nil, false
			}
			lines = updated
		}
		contents[fileEdit.Path] = lines
	}

	changed := make([]string, 0, len(fileEdits))
	for _, fileEdit := range fileEdits {
		body := strings.Join(contents[fileEdit.Path], "\n") + "\n"
		if err := os.WriteFile(fileEdit.Path, []byte(body), 0o644); err != nil {
			return nil, false
		}
		changed = append(changed, i.relativePath(fileEdit.Path))
	}
	return changed, true
}

// applyPositionedEdit replaces one range. Edits arrive ordered last-to-first,
// so each one can assume the positions of those not yet applied are still
// valid.
func applyPositionedEdit(lines []string, edit lsp.PositionedEdit) ([]string, bool) {
	if edit.StartLine < 1 || edit.EndLine < edit.StartLine || edit.EndLine > len(lines) {
		return nil, false
	}

	startLine := lines[edit.StartLine-1]
	endLine := lines[edit.EndLine-1]

	start := lsp.ByteOffsetForColumn(startLine, edit.StartColumn)
	end := lsp.ByteOffsetForColumn(endLine, edit.EndColumn)
	if start > len(startLine) || end > len(endLine) {
		return nil, false
	}

	replaced := startLine[:start] + edit.NewText + endLine[end:]

	out := make([]string, 0, len(lines))
	out = append(out, lines[:edit.StartLine-1]...)
	out = append(out, splitLines(replaced)...)
	out = append(out, lines[edit.EndLine:]...)
	return out, true
}

// lineAt reads one line of a file, for converting a byte column into the
// UTF-16 column LSP positions use. The line's own bytes are required for that
// conversion, so there is no way to skip the read.
func (i *Index) lineAt(absolute string, line int) string {
	data, err := os.ReadFile(absolute)
	if err != nil {
		return ""
	}
	lines := splitLines(string(data))
	if line < 1 || line > len(lines) {
		return ""
	}
	return lines[line-1]
}

// LanguageServerDiagnostics reports what a language server has published for a
// file. Unlike references and rename it is not a fallback chain: a server
// either has something to say or does not.
func (i *Index) LanguageServerDiagnostics(path string) ([]protocol.Diagnostic, bool) {
	ctx := context.Background()
	absolute := i.resolvePath(path)

	client, _, ok := i.languageClient(ctx, path)
	if !ok {
		return nil, false
	}

	published := client.Diagnostics(absolute)
	out := make([]protocol.Diagnostic, 0, len(published))
	for _, diagnostic := range published {
		out = append(out, protocol.Diagnostic{
			Level:   severityLevel(diagnostic.Severity),
			Path:    i.relativePath(absolute),
			Line:    diagnostic.Range.Start.Line + 1,
			Column:  diagnostic.Range.Start.Character + 1,
			Message: diagnostic.Message,
		})
	}
	return out, true
}

// severityLevel maps LSP's numeric severity onto jade's vocabulary. Hints are
// reported as info rather than dropped: a server that bothered to say
// something about a line is usually worth surfacing, and jade's renderer
// already orders errors first.
func severityLevel(severity int) protocol.DiagnosticLevel {
	switch severity {
	case lsp.SeverityError:
		return protocol.DiagnosticError
	case lsp.SeverityWarning:
		return protocol.DiagnosticWarning
	default:
		return protocol.DiagnosticInfo
	}
}

// languageServerAvailable reports whether path's language has a running
// server, for explaining why an answer is approximate.
func (i *Index) languageServerAvailable(path string) bool {
	if i.servers == nil {
		return false
	}
	language := lsp.LanguageForPath(filepath.Base(path))
	if language == "" {
		return false
	}
	return i.servers.Unavailable(language) == ""
}

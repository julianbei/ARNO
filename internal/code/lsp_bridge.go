package code

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/julianbei/jade/internal/lsp"
	"github.com/julianbei/jade/internal/pathguard"
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
	absolute, pathErr := i.resolvePath(symbol.Path)
	if pathErr != nil {
		return nil, "", false
	}

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
func (i *Index) languageServerRename(symbol Symbol, line int, column int, newName string) ([]string, error) {
	ctx := context.Background()
	absolute, pathErr := i.resolvePath(symbol.Path)
	if pathErr != nil {
		return nil, pathErr
	}

	client, language, ok := i.languageClient(ctx, symbol.Path)
	if !ok {
		return nil, lsp.ErrNoServer
	}

	lineText := i.lineAt(absolute, line)
	fileEdits, err := lsp.Rename(ctx, client, absolute, lineText, line, column, newName)
	if err == nil && len(fileEdits) == 0 {
		err = lsp.ErrRenameNoEdits
	}

	// A server that declined is asked no further, but the language may have
	// another one installed that can do it. ruby-lsp renames classes and
	// returns null for methods; solargraph renames methods. The primary's
	// refusal is kept as the reported reason if every fallback declines too.
	if errors.Is(err, lsp.ErrRenameNoEdits) || errors.Is(err, lsp.ErrRenameUnsupported) {
		spec, _ := lsp.SpecFor(language)
		for _, fallback := range i.servers.FallbacksFor(ctx, language) {
			if syncErr := i.servers.Sync(fallback, symbol.Path, spec.LanguageID); syncErr != nil {
				continue
			}
			edits, fallbackErr := lsp.Rename(ctx, fallback, absolute, lineText, line, column, newName)
			if fallbackErr == nil && len(edits) > 0 {
				fileEdits, err = edits, nil
				break
			}
		}
	}
	if err != nil {
		return nil, err
	}

	// Preflight every file before writing any of them. A rename that succeeds
	// in three files and fails in the fourth leaves the workspace in a state
	// that compiles nowhere and that nobody asked for.
	contents := make(map[string][]string, len(fileEdits))
	for _, fileEdit := range fileEdits {
		// The server chooses these paths, not the caller. A server confused
		// by a symlink or a vendored copy must not get Jade to write outside.
		if !pathguard.Contains(i.root, fileEdit.Path) {
			return nil, fmt.Errorf("language server proposed an edit to %s, outside the workspace root %s; nothing was written", fileEdit.Path, i.root)
		}
		data, err := os.ReadFile(fileEdit.Path)
		if err != nil {
			return nil, err
		}
		contents[fileEdit.Path] = splitLines(string(data))
	}

	for _, fileEdit := range fileEdits {
		lines := contents[fileEdit.Path]
		for _, edit := range fileEdit.Edits {
			updated, ok := applyPositionedEdit(lines, edit)
			if !ok {
				return nil, fmt.Errorf("language server returned an edit that does not fit %s", fileEdit.Path)
			}
			lines = updated
		}
		contents[fileEdit.Path] = lines
	}

	changed := make([]string, 0, len(fileEdits))
	for _, fileEdit := range fileEdits {
		body := strings.Join(contents[fileEdit.Path], "\n") + "\n"
		if err := os.WriteFile(fileEdit.Path, []byte(body), 0o644); err != nil {
			return nil, err
		}
		changed = append(changed, i.relativePath(fileEdit.Path))
	}
	return changed, nil
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

// diagnosticsWait bounds how long an edit waits for a server to publish
// diagnostics for the new content, after the server has settled. Servers that
// are indexed publish within a few hundred milliseconds; one that has said
// nothing after this is reported as not having checked, rather than holding
// every edit hostage.
const diagnosticsWait = 4 * time.Second

// LanguageServerDiagnostics checks a file with its language server after an
// edit.
//
// It returns the diagnostics, the name of the server that produced them, and,
// when nothing checked the file, why not. handled is false for a file with no
// language at all (Markdown, a Dockerfile), for which saying "not checked" on
// every edit would be noise rather than information.
// withSyntaxFallback answers for a file whose language server cannot run: a
// tree-sitter syntax check when Jade has the grammar, otherwise the reason
// the file went unchecked.
func (i *Index) withSyntaxFallback(path string, reason string) ([]protocol.Diagnostic, string, string, bool) {
	if diagnostics, ok := i.syntaxDiagnostics(path); ok {
		return diagnostics, syntaxCheckerName(reason), "", true
	}
	return nil, "", reason, true
}

func (i *Index) LanguageServerDiagnostics(path string) (diagnostics []protocol.Diagnostic, checker string, unchecked string, handled bool) {
	language := lsp.LanguageForPath(path)
	if language == "" {
		return nil, "", "", false
	}
	if i.servers == nil {
		return i.withSyntaxFallback(path, "no language servers are configured")
	}

	ctx := context.Background()
	client, ok := i.servers.ClientFor(ctx, language)
	if !ok {
		reason := i.servers.Unavailable(language)
		if reason == "" {
			reason = "no language server for " + language
		}
		return i.withSyntaxFallback(path, reason)
	}

	absolute, pathErr := i.resolvePath(path)
	if pathErr != nil {
		return nil, "", pathErr.Error(), true
	}
	// Generation before the sync, so only a publish for the new content
	// counts.
	before := client.DiagnosticsGeneration(absolute)
	endedBefore := client.ProgressEnded()
	spec, _ := lsp.SpecFor(language)
	if err := i.servers.Sync(client, path, spec.LanguageID); err != nil {
		return nil, "", fmt.Sprintf("could not send the file to %s: %v", client.Command(), err), true
	}

	if spec.DiagnosticsAfterCompile {
		published, compiled := client.WaitForCompiledDiagnostics(ctx, absolute, endedBefore, diagnosticsWait)
		if !compiled {
			reason := fmt.Sprintf("%s has not compiled this change yet", client.Command())
			if client.DeclinedPrompt("import the build") != "" {
				reason = fmt.Sprintf("%s needs a build import to report errors; set %s=1 to allow it (runs the build tool and creates .bloop/ and .metals/)",
					client.Command(), lsp.MetalsImportEnv)
			}
			if busy := client.Busy(); busy != "" {
				reason = fmt.Sprintf("%s is still working (%s); diagnostics not available yet", client.Command(), busy)
			}
			return nil, "", reason, true
		}
		return i.convertDiagnostics(absolute, published), client.Command(), "", true
	}
	client.WaitSettled(ctx)

	// Pull when the server implements it, push otherwise. A pull server may
	// never publish, so waiting for a publish from one reports the file
	// unchecked after the full timeout.
	published, fresh := lsp.PullDiagnostics(ctx, client, absolute)
	if !fresh {
		published, fresh = client.WaitForDiagnostics(ctx, absolute, before, diagnosticsWait)
	}
	if !fresh {
		if busy := client.Busy(); busy != "" {
			return nil, "", fmt.Sprintf("%s is still working (%s); diagnostics not available yet", client.Command(), busy), true
		}
		return nil, "", fmt.Sprintf("%s did not report on this file within %s", client.Command(), diagnosticsWait), true
	}

	return i.convertDiagnostics(absolute, published), client.Command(), "", true
}

// convertDiagnostics maps a server's diagnostics for one file into jade's
// 1-based, workspace-relative form.
func (i *Index) convertDiagnostics(absolute string, published []lsp.Diagnostic) []protocol.Diagnostic {
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
	return out
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

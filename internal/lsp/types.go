package lsp

import (
	"net/url"
	"path/filepath"
	"strings"
)

// The subset of LSP ARNO actually uses. Kept minimal on purpose: every field
// here is one ARNO reads or sends, so an unused field is a question about
// whether something was forgotten rather than decoration.

type Position struct {
	// Line is 0-based, unlike every line number ARNO shows a caller.
	Line int `json:"line"`
	// Character is 0-based and counted in UTF-16 code units by default, not
	// bytes and not runes. See utf16Column.
	Character int `json:"character"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

type VersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type ReferenceParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	Context      ReferenceContext       `json:"context"`
}

type ReferenceContext struct {
	IncludeDeclaration bool `json:"includeDeclaration"`
}

type RenameParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	NewName      string                 `json:"newName"`
}

// WorkspaceEdit is a rename's answer. Servers may reply with either shape:
// Changes keyed by URI, or DocumentChanges as an ordered list. Both are
// accepted because which one arrives depends on the client capabilities the
// server believes it negotiated, and getting that wrong silently loses edits.
type WorkspaceEdit struct {
	Changes         map[string][]TextEdit `json:"changes,omitempty"`
	DocumentChanges []TextDocumentEdit    `json:"documentChanges,omitempty"`
}

type TextDocumentEdit struct {
	TextDocument VersionedTextDocumentIdentifier `json:"textDocument"`
	Edits        []TextEdit                      `json:"edits"`
}

type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"`
	Code     any    `json:"code,omitempty"`
	Source   string `json:"source,omitempty"`
	Message  string `json:"message"`
}

// Severity levels as LSP numbers them.
const (
	SeverityError       = 1
	SeverityWarning     = 2
	SeverityInformation = 3
	SeverityHint        = 4
)

type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Version     int          `json:"version,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type DidChangeTextDocumentParams struct {
	TextDocument   VersionedTextDocumentIdentifier  `json:"textDocument"`
	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

// TextDocumentContentChangeEvent carries a whole-document replacement. ARNO
// edits files on disk and re-reads them, so it never has an incremental delta
// to send; full sync is both simpler and the honest description of what
// happened.
type TextDocumentContentChangeEvent struct {
	Text string `json:"text"`
}

// pathToURI converts a filesystem path to the file:// URI servers expect.
//
// url.URL is used rather than string concatenation because the path has to be
// percent-encoded: a directory with a space or a '#' in it produces a URI the
// server silently fails to match otherwise, and the resulting "no references
// found" looks like a correct answer.
func pathToURI(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		absolute = path
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}
	return u.String()
}

// uriToPath converts back, tolerating the unescaped URIs some servers emit.
func uriToPath(uri string) string {
	if !strings.HasPrefix(uri, "file://") {
		return uri
	}
	parsed, err := url.Parse(uri)
	if err != nil {
		return strings.TrimPrefix(uri, "file://")
	}
	path := parsed.Path
	if path == "" {
		path = strings.TrimPrefix(uri, "file://")
	}
	return filepath.FromSlash(path)
}

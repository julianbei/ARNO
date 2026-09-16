package lsp

import (
	"context"
	"encoding/json"
)

// documentDiagnosticReport is the answer to textDocument/diagnostic. Kind is
// "full" with Items, or "unchanged" when the server has nothing new — which
// cannot happen here, since no previous result ID is ever sent.
type documentDiagnosticReport struct {
	Kind  string       `json:"kind"`
	Items []Diagnostic `json:"items"`
}

// PullDiagnostics asks the server for a file's diagnostics, for servers that
// implement the pull model (LSP 3.17) instead of publishing.
//
// ruby-lsp is one: it advertises diagnosticProvider and never sends
// publishDiagnostics, so waiting for a publish after an edit waits out the
// whole timeout and reports the file unchecked — for a server that would have
// answered immediately if asked.
func PullDiagnostics(ctx context.Context, client *Client, path string) ([]Diagnostic, bool) {
	if client == nil || !client.Supports("diagnosticProvider") {
		return nil, false
	}

	ctx, cancel := context.WithTimeout(ctx, RequestTimeout)
	defer cancel()

	// Flush before pulling. ruby-lsp parses a document on its reader thread
	// when a request arrives, but applies didChange later on a worker thread.
	// A pull sent straight after a change is therefore parsed against the old
	// source, answered from that stale parse, and the empty answer is cached
	// until the next edit. Any request's round trip proves the worker has
	// applied the change, after which the pull sees the new source. Verified
	// against ruby-lsp 0.26: a broken edit pulled directly returns no items;
	// pulled after a documentSymbol round trip, it returns the Prism errors.
	if client.Supports("documentSymbolProvider") {
		_ = client.Call(ctx, "textDocument/documentSymbol", map[string]any{
			"textDocument": TextDocumentIdentifier{URI: pathToURI(path)},
		}, nil)
	}

	var report documentDiagnosticReport
	err := client.Call(ctx, "textDocument/diagnostic", map[string]any{
		"textDocument": TextDocumentIdentifier{URI: pathToURI(path)},
	}, &report)
	if err != nil {
		return nil, false
	}
	return report.Items, true
}

// WantsSave reports whether the server asked to be told when a document is
// saved.
//
// arno edits files on disk, so every edit is a save and saying so is the
// truthful description. It matters because some servers only analyse on save:
// metals compiles then, and a change notification alone produced an empty
// diagnostic publish for a file with a syntax error in it.
func (c *Client) WantsSave() bool {
	raw, ok := c.capabilities["textDocumentSync"]
	if !ok {
		return false
	}
	var options struct {
		Save json.RawMessage `json:"save"`
	}
	// A bare number is the legacy sync kind with no save options.
	if err := json.Unmarshal(raw, &options); err != nil || len(options.Save) == 0 {
		return false
	}
	var enabled bool
	if err := json.Unmarshal(options.Save, &enabled); err == nil {
		return enabled
	}
	// An options object ({"includeText": ...}) means yes.
	return string(options.Save) != "null"
}

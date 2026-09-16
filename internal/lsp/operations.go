package lsp

import (
	"context"
	"errors"
	"sort"
	"unicode/utf16"
	"unicode/utf8"
)

// References asks the server for every use of the symbol at a position.
//
// Line and column are arno's own 1-based, byte-oriented coordinates; the
// conversion to LSP's 0-based UTF-16 positions happens here so no caller has
// to know about it.
func References(ctx context.Context, client *Client, path string, lineText string, line int, column int) ([]Location, bool) {
	if client == nil {
		return nil, false
	}
	if !client.Supports("referencesProvider") {
		return nil, false
	}
	client.WaitSettled(ctx)

	ctx, cancel := context.WithTimeout(ctx, RequestTimeout)
	defer cancel()

	var locations []Location
	err := client.Call(ctx, "textDocument/references", ReferenceParams{
		TextDocument: TextDocumentIdentifier{URI: pathToURI(path)},
		Position:     Position{Line: line - 1, Character: utf16Column(lineText, column)},
		Context:      ReferenceContext{IncludeDeclaration: false},
	}, &locations)
	if err != nil {
		return nil, false
	}

	sort.Slice(locations, func(a, b int) bool {
		if locations[a].URI == locations[b].URI {
			return locations[a].Range.Start.Line < locations[b].Range.Start.Line
		}
		return locations[a].URI < locations[b].URI
	})
	return locations, true
}

// Rename failures a caller must be able to tell apart. "No server" means
// install one; "unsupported" and "no edits" mean the server is there and
// declined, which is a different action entirely.
var (
	ErrNoServer          = errors.New("no language server is running for this file")
	ErrRenameUnsupported = errors.New("the language server does not implement rename")
	ErrRenameNoEdits     = errors.New("the language server produced no edits for this symbol")
)

// FileEdit is one file's worth of a rename, in arno's coordinates.
type FileEdit struct {
	Path string
	// Edits are ordered last-to-first within the file, so applying them in
	// sequence never invalidates the offsets of those not yet applied.
	Edits []PositionedEdit
}

// PositionedEdit is a replacement addressed by 1-based line and byte column.
type PositionedEdit struct {
	StartLine   int
	StartColumn int
	EndLine     int
	EndColumn   int
	NewText     string
}

// Rename asks the server to rename the symbol at a position, returning the
// edits rather than applying them.
//
// Returning edits rather than writing files keeps the decision where it
// belongs: arno's edit service owns revisions, formatting and validation, and
// a rename that wrote files behind its back would produce a change with no
// revision bump and no diagnostics.
func Rename(ctx context.Context, client *Client, path string, lineText string, line int, column int, newName string) ([]FileEdit, error) {
	if client == nil {
		return nil, ErrNoServer
	}
	if !client.Supports("renameProvider") {
		return nil, ErrRenameUnsupported
	}
	client.WaitSettled(ctx)

	ctx, cancel := context.WithTimeout(ctx, RequestTimeout)
	defer cancel()

	var edit WorkspaceEdit
	err := client.Call(ctx, "textDocument/rename", RenameParams{
		TextDocument: TextDocumentIdentifier{URI: pathToURI(path)},
		Position:     Position{Line: line - 1, Character: utf16Column(lineText, column)},
		NewName:      newName,
	}, &edit)
	if err != nil {
		// The server's own words. Reporting "no language server is
		// available" for a server that is running and answered is a lie that
		// sends the reader to install something they already have.
		return nil, err
	}

	byPath := make(map[string][]TextEdit)
	for uri, edits := range edit.Changes {
		byPath[uriToPath(uri)] = append(byPath[uriToPath(uri)], edits...)
	}
	for _, documentEdit := range edit.DocumentChanges {
		path := uriToPath(documentEdit.TextDocument.URI)
		byPath[path] = append(byPath[path], documentEdit.Edits...)
	}
	if len(byPath) == 0 {
		return nil, ErrRenameNoEdits
	}

	out := make([]FileEdit, 0, len(byPath))
	for path, edits := range byPath {
		// Descending order matters: applying an edit shifts everything after
		// it, so edits must be applied from the end of the file backwards for
		// the remaining positions to stay valid.
		sort.Slice(edits, func(a, b int) bool {
			if edits[a].Range.Start.Line == edits[b].Range.Start.Line {
				return edits[a].Range.Start.Character > edits[b].Range.Start.Character
			}
			return edits[a].Range.Start.Line > edits[b].Range.Start.Line
		})

		positioned := make([]PositionedEdit, 0, len(edits))
		for _, e := range edits {
			positioned = append(positioned, PositionedEdit{
				StartLine:   e.Range.Start.Line + 1,
				StartColumn: e.Range.Start.Character,
				EndLine:     e.Range.End.Line + 1,
				EndColumn:   e.Range.End.Character,
				NewText:     e.NewText,
			})
		}
		out = append(out, FileEdit{Path: path, Edits: positioned})
	}

	sort.Slice(out, func(a, b int) bool { return out[a].Path < out[b].Path })
	return out, nil
}

// utf16Column converts a 1-based byte column into the 0-based UTF-16 code unit
// offset LSP positions use by default.
//
// This is the detail that makes positions wrong in exactly the files where a
// wrong answer is hardest to spot. A line containing "café" or an emoji shifts
// every column after it: a byte offset of 6 might be UTF-16 offset 5, and the
// server then answers about the wrong identifier — confidently, with no error.
// ASCII lines are unaffected, which is why this survives casual testing.
func utf16Column(lineText string, byteColumn int) int {
	if byteColumn <= 1 {
		return 0
	}
	offset := byteColumn - 1
	if offset > len(lineText) {
		offset = len(lineText)
	}

	units := 0
	for index := 0; index < offset; {
		r, size := utf8.DecodeRuneInString(lineText[index:])
		if r == utf8.RuneError && size <= 1 {
			units++
			index++
			continue
		}
		units += len(utf16.Encode([]rune{r}))
		index += size
	}
	return units
}

// byteColumn is utf16Column's inverse, for turning a server's answer back into
// a position ARNO can use against the file's bytes.
func byteColumn(lineText string, utf16Offset int) int {
	if utf16Offset <= 0 {
		return 1
	}

	units := 0
	for index := 0; index < len(lineText); {
		if units >= utf16Offset {
			return index + 1
		}
		r, size := utf8.DecodeRuneInString(lineText[index:])
		if r == utf8.RuneError && size <= 1 {
			units++
			index++
			continue
		}
		units += len(utf16.Encode([]rune{r}))
		index += size
	}
	return len(lineText) + 1
}

// URIPath converts a file:// URI to a filesystem path. Exported because
// callers outside this package receive Locations and need to address the
// files they name.
func URIPath(uri string) string {
	return uriToPath(uri)
}

// ByteOffsetForColumn converts a 0-based UTF-16 column, as a server reports
// it, into a 0-based byte offset into the line. The inverse of what is sent
// when asking a question, and needed to apply what comes back.
func ByteOffsetForColumn(lineText string, utf16Offset int) int {
	return byteColumn(lineText, utf16Offset) - 1
}

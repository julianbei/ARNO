package render

import (
	"fmt"
	"strings"
	"testing"

	"github.com/julianbei/arno/internal/protocol"
)

func TestUnknownTypeFallsBackToJSON(t *testing.T) {
	// An unrendered type must degrade to the caller's JSON path rather than
	// being rendered by some guessed default that silently drops fields.
	if _, ok := Text(struct{ X int }{1}); ok {
		t.Fatalf("expected no renderer for an unknown type")
	}
}

func TestInspectEmitsSourceAsSourceNotAsQuotedString(t *testing.T) {
	source := "func Example() {\n\treturn\n}"
	out, ok := Text(protocol.InspectResponse{Revision: "r3", Source: source})
	if !ok {
		t.Fatalf("expected a renderer for InspectResponse")
	}
	if !strings.Contains(out, source) {
		t.Fatalf("expected the source verbatim, got:\n%s", out)
	}
	// The single largest saving: JSON escapes every newline and tab.
	if strings.Contains(out, `\n`) || strings.Contains(out, `\t`) {
		t.Fatalf("expected real newlines and tabs, got escaped ones:\n%s", out)
	}
}

func TestInspectOmitsEmptyStructure(t *testing.T) {
	out, _ := Text(protocol.InspectResponse{Revision: "r1", Source: "x"})
	for _, unwanted := range []string{"Sections", "Outline", "Resolve", "null", "Imports"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("expected %q to be omitted entirely, got:\n%s", unwanted, out)
		}
	}
}

func TestInspectLeadsWithAmbiguity(t *testing.T) {
	// An ambiguous symbol IS the answer; it must not be buried under an
	// outline the caller cannot use yet.
	out, _ := Text(protocol.InspectResponse{
		Resolve: protocol.SymbolResolution{
			Status:       protocol.ResolutionAmbiguous,
			Query:        "Target",
			CandidateIDs: []string{"a.go::Target@3", "b.go::Target@7"},
		},
		Outline: []protocol.OutlineItem{{Name: "Noise", Kind: "func", From: 1, To: 2}},
	})
	if !strings.HasPrefix(out, "ambiguous: Target") {
		t.Fatalf("expected the ambiguity first, got:\n%s", out)
	}
	if strings.Contains(out, "Noise") {
		t.Fatalf("expected unusable outline content to be suppressed, got:\n%s", out)
	}
}

// A drift count on a read is a line no reader can act on, printed on every
// call. Reads return current content regardless, and changes() reports what
// moved.
func TestReadsCarryNoDriftCountOrPathLists(t *testing.T) {
	paths := []string{"a.go", "b.go", "c.go"}
	out, _ := Text(protocol.InspectResponse{
		Revision: "r2",
		Source:   "x",
		Freshness: protocol.Freshness{
			Drifted:      true,
			ChangedPaths: paths,
			DirtyPaths:   paths,
		},
	})
	if strings.Contains(out, "drift") {
		t.Fatalf("expected no drift line on a read, got:\n%s", out)
	}
	if strings.Contains(out, "b.go") {
		t.Fatalf("expected path lists to be dropped, got:\n%s", out)
	}
	if !strings.HasPrefix(out, "r2\n") {
		t.Fatalf("expected the header reduced to the revision, got:\n%s", out)
	}
}

// Broken freshness is the one case worth a line: Arno's own view of the
// workspace cannot be trusted.
func TestUnknownFreshnessIsStillReported(t *testing.T) {
	out, _ := Text(protocol.InspectResponse{
		Revision:  "r1",
		Source:    "x",
		Freshness: protocol.Freshness{Unknown: "git status failed"},
	})
	if !strings.Contains(out, "freshness unknown: git status failed") {
		t.Fatalf("expected unknown freshness surfaced, got:\n%s", out)
	}
}

func TestChangesPrintsEachFileExactlyOnce(t *testing.T) {
	// Files is the only path list in the response now; there is no second
	// Paths list to accidentally print alongside it.
	out, _ := Text(protocol.ChangesResponse{
		Files:   []protocol.ChangedFile{{Path: "a.go", Added: 3, Removed: 1}, {Path: "b.go", Added: 2}},
		Summary: "2 files, +5 -1",
	})
	if strings.Count(out, "a.go") != 1 {
		t.Fatalf("expected each path exactly once, got:\n%s", out)
	}
	if !strings.Contains(out, "+3 -1 a.go") {
		t.Fatalf("expected per-file counts, got:\n%s", out)
	}
}
func TestChangesStillListsFilesWithNoGitDelta(t *testing.T) {
	// A file arno touched that git reports no delta for arrives as a zero-count
	// entry. It must still be listed: the renderer is the last place that could
	// silently drop it, and "no changes" would be wrong.
	out, _ := Text(protocol.ChangesResponse{
		Revision: "r7",
		Files:    []protocol.ChangedFile{{Path: "touched.go"}},
		Summary:  "1 files, +0 -0",
	})
	if !strings.Contains(out, "touched.go") {
		t.Fatalf("expected the touched file listed, got:\n%s", out)
	}
	if strings.Contains(out, "no changes") {
		t.Fatalf("expected it not to read as a clean tree, got:\n%s", out)
	}
}

func TestWorkspaceTreeRendersBarePaths(t *testing.T) {
	out, _ := Text(protocol.WorkspaceTreeResponse{
		Entries: []protocol.WorkspaceTreeEntry{
			{Path: "cmd", IsDir: true},
			{Path: "cmd/main.go"},
		},
	})
	if out != "cmd/\ncmd/main.go" {
		t.Fatalf("expected bare paths with a trailing slash for directories, got:\n%q", out)
	}
}

func TestReferencesShowsLineAndConfidence(t *testing.T) {
	out, _ := Text(protocol.ReferencesResponse{
		Query:  "Target",
		Source: "lsp",
		References: []protocol.ReferenceLocation{
			{Path: "caller.go", Line: 4, Column: 9},
			{Path: "other.go", Confidence: "unique-name"},
		},
	})
	if !strings.Contains(out, "caller.go:4:9") {
		t.Fatalf("expected a precise location, got:\n%s", out)
	}
	if !strings.Contains(out, "other.go (unique-name)") {
		t.Fatalf("expected the confidence label, got:\n%s", out)
	}
}

func TestEditLeadsWithRevisionAndSurfacesDiagnostics(t *testing.T) {
	out, _ := Text(protocol.EditResponse{
		OldRevision:  "r1",
		NewRevision:  "r2",
		Changed:      []string{"main.go"},
		AddedLines:   4,
		RemovedLines: 2,
		Diagnostics: []protocol.Diagnostic{
			{Level: protocol.DiagnosticError, Path: "main.go", Line: 9, Column: 3, Message: "undefined: X"},
		},
		Jobs: []string{"job-1"},
	})
	if !strings.HasPrefix(out, "r1 → r2 · main.go · +4 -2") {
		t.Fatalf("unexpected header:\n%s", out)
	}
	// Diagnostics are the reason an agent reads an edit response at all.
	if !strings.Contains(out, "error main.go:9:3 undefined: X") {
		t.Fatalf("expected the diagnostic on its own line, got:\n%s", out)
	}
	// The background job ID is not something a reader of this response acts
	// on; it stays in JSON output only.
	if strings.Contains(out, "job-1") || strings.Contains(out, "jobs:") {
		t.Fatalf("expected no job line in an edit response, got:\n%s", out)
	}
}

func TestJobOutputReportsOmission(t *testing.T) {
	out, _ := Text(protocol.JobOutputResponse{
		ID: "job-3", Kind: "tests", Status: "completed",
		RawOutput: "FAIL", OmittedBytes: 1200,
	})
	if !strings.Contains(out, "1200 bytes omitted") {
		t.Fatalf("expected truncation to stay visible, got:\n%s", out)
	}
}

func TestRenderedOutputIsSubstantiallySmallerThanJSON(t *testing.T) {
	// The whole justification for this package, asserted rather than assumed.
	response := protocol.InspectResponse{
		Revision: "r3",
		Source:   "func Example() {\n\treturn\n}",
		Freshness: protocol.Freshness{
			Drifted:      true,
			ChangedPaths: make([]string, 35),
			DirtyPaths:   make([]string, 35),
		},
	}
	out, _ := Text(response)
	if len(out) > 120 {
		t.Fatalf("expected a compact rendering, got %d bytes:\n%s", len(out), out)
	}
}

func TestChangesNestsSymbolsUnderTheirFile(t *testing.T) {
	out, _ := Text(protocol.ChangesResponse{
		Summary: "1 files, +5 -1",
		Files:   []protocol.ChangedFile{{Path: "a.go", Added: 5, Removed: 1}},
		Symbols: []protocol.SymbolChange{
			{Path: "a.go", Symbol: "Added", Kind: "func", Change: protocol.SymbolAdded},
			{Path: "a.go", Symbol: "Gone", Kind: "func", Change: protocol.SymbolRemoved},
		},
	})
	if !strings.Contains(out, "  added func Added") {
		t.Fatalf("expected the symbol nested under its file, got:\n%s", out)
	}
	// The path is already on the line above; repeating it on every symbol
	// line is exactly the duplication Phase 10 removed.
	if strings.Count(out, "a.go") != 1 {
		t.Fatalf("expected the path named once, got:\n%s", out)
	}
}

func TestChangesCapsSymbolDetailButReportsTheTrueTotal(t *testing.T) {
	symbols := make([]protocol.SymbolChange, 100)
	for i := range symbols {
		symbols[i] = protocol.SymbolChange{
			Path: "a.go", Symbol: fmt.Sprintf("S%d", i), Kind: "func", Change: protocol.SymbolAdded,
		}
	}
	out, _ := Text(protocol.ChangesResponse{
		Files:   []protocol.ChangedFile{{Path: "a.go"}},
		Symbols: symbols,
	})

	if strings.Count(out, "added func") > maxRenderedSymbolChanges {
		t.Fatalf("expected symbol detail to be capped, got:\n%s", out)
	}
	// The cap must cost detail, never accuracy.
	if !strings.Contains(out, "60 more symbol changes") {
		t.Fatalf("expected the true remaining count, got:\n%s", out)
	}
}

func TestReferencesRenderNamesCallerWhenThereIsNoLine(t *testing.T) {
	out, _ := Text(protocol.ReferencesResponse{
		Query:  "Target",
		Source: "approximate",
		References: []protocol.ReferenceLocation{
			{Path: "caller.go", Symbol: "First", Confidence: "unique-name"},
			{Path: "caller.go", Symbol: "Second", Confidence: "unique-name"},
		},
	})
	// The two lines must not be identical — that was the whole bug.
	if !strings.Contains(out, "caller.go First") || !strings.Contains(out, "caller.go Second") {
		t.Fatalf("expected each caller named, got:\n%s", out)
	}
}

func TestReferencesRenderOmitsSymbolWhenLineIsPresent(t *testing.T) {
	out, _ := Text(protocol.ReferencesResponse{
		Source: "lsp",
		References: []protocol.ReferenceLocation{
			{Path: "caller.go", Line: 4, Column: 9},
		},
	})
	if !strings.Contains(out, "caller.go:4:9") {
		t.Fatalf("expected the precise location, got:\n%s", out)
	}
}
func manyChangedFiles(count int) []protocol.ChangedFile {
	files := make([]protocol.ChangedFile, 0, count)
	for i := 0; i < count; i++ {
		files = append(files, protocol.ChangedFile{
			Path:  fmt.Sprintf("pkg/file%03d.go", i),
			Added: 1,
		})
	}
	return files
}

func TestChangesCapsTheFileList(t *testing.T) {
	// The file list was never bounded while symbols always were, which made
	// changes() the most expensive call arno makes on a large branch.
	out, _ := Text(protocol.ChangesResponse{
		Revision: "r9",
		Files:    manyChangedFiles(88),
		Summary:  "88 files, +88 -0",
	})

	if strings.Contains(out, "file087.go") {
		t.Fatalf("expected the list capped, got:\n%s", out)
	}
	if !strings.Contains(out, "48 more files") {
		t.Fatalf("expected the remainder reported, got:\n%s", out)
	}
	// A cap must cost detail, never accuracy: the true total still leads.
	if !strings.Contains(out, "88 files") {
		t.Fatalf("expected the true file count in the summary, got:\n%s", out)
	}
}

func TestChangesDoesNotCapASmallFileList(t *testing.T) {
	out, _ := Text(protocol.ChangesResponse{
		Files:   manyChangedFiles(5),
		Summary: "5 files, +5 -0",
	})
	if strings.Contains(out, "more files") {
		t.Fatalf("expected no truncation notice, got:\n%s", out)
	}
	if !strings.Contains(out, "file004.go") {
		t.Fatalf("expected every file listed, got:\n%s", out)
	}
}

func TestChangesSaysWhenSymbolsWereDeliberatelyOmitted(t *testing.T) {
	// "No symbol lines" would otherwise read as "no symbols changed", which is
	// a much stronger and quite wrong claim.
	out, _ := Text(protocol.ChangesResponse{
		Revision:       "r9",
		Files:          manyChangedFiles(88),
		SymbolsOmitted: 88,
		Summary:        "88 files, +88 -0",
	})
	if !strings.Contains(out, "symbol changes omitted") {
		t.Fatalf("expected the omission stated, got:\n%s", out)
	}
	if !strings.Contains(out, "outline") {
		t.Fatalf("expected a way to get the detail anyway, got:\n%s", out)
	}
}

func TestChangesStaysSilentWhenSymbolsSimplyDidNotChange(t *testing.T) {
	// The other half of the distinction: an empty Symbols with no omission is
	// a real "nothing to report", and must not claim anything was skipped.
	out, _ := Text(protocol.ChangesResponse{
		Files:   manyChangedFiles(3),
		Summary: "3 files, +3 -0",
	})
	if strings.Contains(out, "omitted") {
		t.Fatalf("expected no omission notice, got:\n%s", out)
	}
}
func TestInspectWarnsBeforeAnIncompleteOutlineNotAfter(t *testing.T) {
	// The failure mode is trusting an absence, so the caveat has to be read
	// before the data. A reader who stops at the declaration they were looking
	// for must already have seen that the list may be incomplete.
	out, _ := Text(protocol.InspectResponse{
		Revision: "r3",
		Outline: []protocol.OutlineItem{
			{Kind: "class", Name: "Greeter", From: 4, To: 6},
		},
		Parser: protocol.ParserInfo{
			Language: "python",
			Parser:   "heuristic",
			Note:     "no python grammar — declarations were found by a text scan and some may be missing",
		},
	})

	note := strings.Index(out, "no python grammar")
	symbol := strings.Index(out, "Greeter")
	if note < 0 {
		t.Fatalf("expected the caveat rendered, got:\n%s", out)
	}
	if note > symbol {
		t.Fatalf("expected the caveat above the outline, got:\n%s", out)
	}
}

func TestInspectSaysNothingWhenTheGrammarParsed(t *testing.T) {
	// Silence is correct in the common case: a caveat on every Go file would be
	// ignored by the time it mattered.
	out, _ := Text(protocol.InspectResponse{
		Revision: "r3",
		Outline:  []protocol.OutlineItem{{Kind: "function", Name: "Present", From: 3, To: 5}},
		Parser:   protocol.ParserInfo{Language: "go", Parser: "grammar", Complete: true},
	})
	if strings.Contains(out, "!") {
		t.Fatalf("expected no caveat for a grammar parse, got:\n%s", out)
	}
}
func TestAmbiguousShowsWhatDistinguishesTheCandidates(t *testing.T) {
	// The measured problem: two methods named Put in one file, told apart only
	// by receiver. Bare IDs forced a whole extra call to find out which was
	// which, and that round trip is why arno lost a head-to-head against grep.
	out, _ := Text(protocol.InspectResponse{
		Resolve: protocol.SymbolResolution{
			Status: protocol.ResolutionAmbiguous,
			Query:  "Put",
			CandidateIDs: []string{
				"internal/cas/store.go::Put@48",
				"internal/cas/store.go::Put@72",
			},
			Candidates: []protocol.SymbolCandidate{
				{ID: "internal/cas/store.go::Put@48", Line: 48, Signature: "func (s *S3CompatibleStore) Put(ctx context.Context) error {"},
				{ID: "internal/cas/store.go::Put@72", Line: 72, Signature: "func (s *Store) Put(ctx context.Context) error {"},
			},
		},
	})

	if !strings.Contains(out, "S3CompatibleStore") || !strings.Contains(out, "(s *Store)") {
		t.Fatalf("expected both receivers shown, got:\n%s", out)
	}
	// The ID still has to be there — it is what the caller passes back.
	if !strings.Contains(out, "internal/cas/store.go::Put@72") {
		t.Fatalf("expected the IDs kept, got:\n%s", out)
	}
}

func TestAmbiguousFallsBackToBareIDs(t *testing.T) {
	// Transports that only populate CandidateIDs must still render something
	// usable rather than an empty list.
	out, _ := Text(protocol.InspectResponse{
		Resolve: protocol.SymbolResolution{
			Status:       protocol.ResolutionAmbiguous,
			Query:        "Put",
			CandidateIDs: []string{"a.go::Put@1", "b.go::Put@2"},
		},
	})
	if !strings.Contains(out, "a.go::Put@1") || !strings.Contains(out, "b.go::Put@2") {
		t.Fatalf("expected the bare IDs, got:\n%s", out)
	}
}

func TestAmbiguousSurvivesAMissingSignature(t *testing.T) {
	// A file that could not be re-read degrades to the ID alone rather than
	// rendering a dangling separator.
	out, _ := Text(protocol.InspectResponse{
		Resolve: protocol.SymbolResolution{
			Status:     protocol.ResolutionAmbiguous,
			Query:      "Put",
			Candidates: []protocol.SymbolCandidate{{ID: "a.go::Put@1", Line: 1}},
		},
	})
	if strings.Contains(out, "  \n") || strings.HasSuffix(out, "  ") {
		t.Fatalf("expected no trailing separator, got:\n%q", out)
	}
	if !strings.Contains(out, "a.go::Put@1") {
		t.Fatalf("expected the ID, got:\n%s", out)
	}
}

// The reported friction: a guessed symbol ID answered with a bare "not found"
// and no way forward. The candidates are already in hand by the time the
// lookup fails, so withholding them only costs the caller another call.
func TestNotFoundOffersTheCandidateID(t *testing.T) {
	out, _ := Text(protocol.InspectResponse{
		Resolve: protocol.SymbolResolution{
			Status:       protocol.ResolutionNotFound,
			Query:        "greet.go::Greet",
			CandidateIDs: []string{"greet.go::Greet@3"},
		},
	})
	if !strings.Contains(out, "did you mean greet.go::Greet@3?") {
		t.Fatalf("expected a suggestion, got: %q", out)
	}
}

func TestNotFoundOffersEveryCandidateWhenSeveralShareTheName(t *testing.T) {
	out, _ := Text(protocol.InspectResponse{
		Resolve: protocol.SymbolResolution{
			Status:       protocol.ResolutionNotFound,
			Query:        "store.go::Put",
			CandidateIDs: []string{"store.go::Put@6", "store.go::Put@8"},
		},
	})
	for _, want := range []string{"did you mean one of:", "store.go::Put@6", "store.go::Put@8"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in: %q", want, out)
		}
	}
}

// A name that is genuinely absent must not acquire an invented suggestion.
func TestNotFoundStaysBareWithNoCandidates(t *testing.T) {
	out, _ := Text(protocol.InspectResponse{
		Resolve: protocol.SymbolResolution{
			Status: protocol.ResolutionNotFound,
			Query:  "greet.go::Nope",
		},
	})
	if strings.Contains(out, "did you mean") {
		t.Fatalf("nothing should be suggested, got: %q", out)
	}
}

// An edit with no diagnostic lines reads as clean only if the response says
// what looked. A file of a known language nothing could check says why.
func TestEditNamesWhatCheckedIt(t *testing.T) {
	out, _ := Text(protocol.EditResponse{
		OldRevision: "r1",
		NewRevision: "r2",
		Changed:     []string{"a.go", "b.py"},
		Checks: protocol.CheckReport{
			Checked:   []string{"gopls"},
			Unchecked: []string{"b.py: pyright-langserver is not installed"},
		},
	})
	if !strings.Contains(strings.SplitN(out, "\n", 2)[0], "checked: gopls") {
		t.Fatalf("expected the checker in the header, got:\n%s", out)
	}
	if !strings.Contains(out, "not checked: b.py: pyright-langserver is not installed") {
		t.Fatalf("expected the unchecked file with its reason, got:\n%s", out)
	}
}

func TestApplyNamesWhatCheckedIt(t *testing.T) {
	out, _ := Text(protocol.ApplyResponse{
		OldRevision: "r1",
		NewRevision: "r2",
		Summary:     "2 edits",
		Checks:      protocol.CheckReport{Checked: []string{"gopls", "typescript-language-server"}},
	})
	if !strings.Contains(out, "checked: gopls, typescript-language-server") {
		t.Fatalf("expected the checkers in the apply header, got:\n%s", out)
	}
}

// YAML parsers report a line but no column. `ci.yml:3:0` reads as a real
// position; the column is left out instead.
func TestDiagnosticWithoutAColumnOmitsIt(t *testing.T) {
	out, _ := Text(protocol.EditResponse{
		OldRevision: "r1",
		NewRevision: "r2",
		Diagnostics: []protocol.Diagnostic{
			{Level: protocol.DiagnosticError, Path: "ci.yml", Line: 3, Message: "invalid YAML: did not find expected node content"},
			{Level: protocol.DiagnosticError, Path: "a.json", Line: 2, Column: 19, Message: "invalid JSON"},
		},
	})
	if !strings.Contains(out, "error ci.yml:3 invalid YAML") || strings.Contains(out, "ci.yml:3:0") {
		t.Fatalf("expected the column omitted when unknown, got:\n%s", out)
	}
	if !strings.Contains(out, "error a.json:2:19 invalid JSON") {
		t.Fatalf("expected a known column kept, got:\n%s", out)
	}
}

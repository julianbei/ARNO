package internalapi

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/julianbei/arno/internal/code"
	"github.com/julianbei/arno/internal/diagnostics"
	"github.com/julianbei/arno/internal/edit"
	"github.com/julianbei/arno/internal/events"
	"github.com/julianbei/arno/internal/jobs"
	"github.com/julianbei/arno/internal/languages"
	"github.com/julianbei/arno/internal/protocol"
	"github.com/julianbei/arno/internal/telemetry"
	"github.com/julianbei/arno/internal/workspace"
)

// Server is a transport-neutral internal API surface for harness integrations.
type Server struct {
	workspace   *workspace.Manager
	index       *code.Index
	edit        *edit.Service
	diagnostics *diagnostics.Service
	jobs        *jobs.Runner
	languages   *languages.Registry
	bus         *events.Bus
	// continuations holds the rest of answers cut by a budget.
	continuations continuationStore
}

func NewServer(
	workspaceManager *workspace.Manager,
	index *code.Index,
	editService *edit.Service,
	diagnosticsService *diagnostics.Service,
	runner *jobs.Runner,
	registry *languages.Registry,
	bus *events.Bus,
) *Server {
	return &Server{
		workspace:   workspaceManager,
		index:       index,
		edit:        editService,
		diagnostics: diagnosticsService,
		jobs:        runner,
		languages:   registry,
		bus:         bus,
	}
}

func (s *Server) Start(context.Context) error {
	return nil
}

// describeCandidates attaches each candidate's declaration line, so an
// ambiguous answer can be acted on without a second call.
//
// The signature is read from the file rather than reconstructed from the
// symbol record, because the record has no receiver and the receiver is
// usually the whole distinction. A read that fails degrades to no signature
// rather than failing the response: a candidate list without signatures is
// still exactly what ARNO returned before this existed.
func (s *Server) describeCandidates(candidates []code.Symbol) []protocol.SymbolCandidate {
	out := make([]protocol.SymbolCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		entry := protocol.SymbolCandidate{
			ID:   candidate.ID,
			Kind: candidate.Kind,
			Path: candidate.Path,
			Line: candidate.From,
		}
		if line, err := s.index.ReadRange(candidate.Path, candidate.From, candidate.From); err == nil {
			entry.Signature = strings.TrimSpace(line)
		}
		out = append(out, entry)
	}
	return out
}

func (s *Server) Outline(req protocol.OutlineRequest) (protocol.InspectResponse, error) {
	sections, symbols, parser, err := s.index.OutlineStructured(req.Path)
	if err != nil {
		return protocol.InspectResponse{}, err
	}

	outline := make([]protocol.OutlineItem, 0, len(symbols))
	for _, symbol := range symbols {
		outline = append(outline, protocol.OutlineItem{
			ID:   symbol.ID,
			Kind: symbol.Kind,
			Name: symbol.Name,
			Path: symbol.Path,
			From: symbol.From,
			To:   symbol.To,
		})
	}

	convert := func(in []code.Symbol) []protocol.OutlineItem {
		out := make([]protocol.OutlineItem, 0, len(in))
		for _, symbol := range in {
			out = append(out, protocol.OutlineItem{
				ID:   symbol.ID,
				Kind: symbol.Kind,
				Name: symbol.Name,
				Path: symbol.Path,
				From: symbol.From,
				To:   symbol.To,
			})
		}
		return out
	}

	freshness := s.workspace.Freshness(req.IndexedCommit)
	return protocol.InspectResponse{
		Revision: s.workspace.Revision(),
		Outline:  outline,
		Parser:   parser,
		Sections: protocol.OutlineSections{
			Imports:   sections.Imports,
			Types:     convert(sections.Types),
			Classes:   convert(sections.Classes),
			Functions: convert(sections.Functions),
			Methods:   convert(sections.Methods),
			Other:     convert(sections.Other),
		},
		Freshness: protocol.Freshness{
			IndexedCommit: freshness.IndexedCommit,
			HeadCommit:    freshness.HeadCommit,
			Drifted:       freshness.Drifted,
			ChangedPaths:  freshness.ChangedPaths,
			DirtyPaths:    freshness.DirtyPaths,
			Unknown:       freshness.Unknown,
		},
	}, nil
}

// readSymbolAll answers read_symbol whole. ReadSymbol pages its body by budget.
func (s *Server) readSymbolAll(req protocol.ReadSymbolRequest) (protocol.InspectResponse, error) {
	freshness := s.workspace.Freshness(req.IndexedCommit)
	base := protocol.InspectResponse{
		Revision: s.workspace.Revision(),
		Freshness: protocol.Freshness{
			IndexedCommit: freshness.IndexedCommit,
			HeadCommit:    freshness.HeadCommit,
			Drifted:       freshness.Drifted,
			ChangedPaths:  freshness.ChangedPaths,
			DirtyPaths:    freshness.DirtyPaths,
			Unknown:       freshness.Unknown,
		},
	}

	if req.SymbolID != "" {
		symbol, source, err := s.index.ReadSymbol(req.Path, req.SymbolID, req.MaxLines)
		if err != nil {
			base.Outline, base.Resolve = s.index.NotFoundResolution(req.Path, req.SymbolID)
			return base, nil
		}

		base.Outline = []protocol.OutlineItem{{
			ID:   symbol.ID,
			Kind: symbol.Kind,
			Name: symbol.Name,
			Path: symbol.Path,
			From: symbol.From,
			To:   symbol.To,
		}}
		base.Source = source
		base.Resolve = protocol.SymbolResolution{
			Status:     protocol.ResolutionExact,
			Query:      req.SymbolID,
			SelectedID: symbol.ID,
		}
		return base, nil
	}

	if req.SymbolName == "" {
		return protocol.InspectResponse{}, fmt.Errorf("symbol id or symbol name is required")
	}

	candidates, err := s.index.SymbolsByName(req.Path, req.SymbolName)
	if err != nil {
		return protocol.InspectResponse{}, err
	}

	if len(candidates) == 0 {
		base.Resolve = protocol.SymbolResolution{
			Status: protocol.ResolutionNotFound,
			Query:  req.SymbolName,
		}
		return base, nil
	}

	candidateIDs := make([]string, 0, len(candidates))
	outline := make([]protocol.OutlineItem, 0, len(candidates))
	for _, candidate := range candidates {
		candidateIDs = append(candidateIDs, candidate.ID)
		outline = append(outline, protocol.OutlineItem{
			ID:   candidate.ID,
			Kind: candidate.Kind,
			Name: candidate.Name,
			Path: candidate.Path,
			From: candidate.From,
			To:   candidate.To,
		})
	}

	if len(candidates) > 1 {
		base.Outline = outline
		base.Resolve = protocol.SymbolResolution{
			Status:       protocol.ResolutionAmbiguous,
			Query:        req.SymbolName,
			CandidateIDs: candidateIDs,
			Candidates:   s.describeCandidates(candidates),
		}
		return base, nil
	}

	symbol, source, err := s.index.ReadSymbol(req.Path, candidates[0].ID, req.MaxLines)
	if err != nil {
		return protocol.InspectResponse{}, err
	}

	return protocol.InspectResponse{
		Outline: []protocol.OutlineItem{
			{
				ID:   symbol.ID,
				Kind: symbol.Kind,
				Name: symbol.Name,
				Path: symbol.Path,
				From: symbol.From,
				To:   symbol.To,
			},
		},
		Revision:  base.Revision,
		Source:    source,
		Freshness: base.Freshness,
		Resolve: protocol.SymbolResolution{
			Status:       protocol.ResolutionExact,
			Query:        req.SymbolName,
			SelectedID:   symbol.ID,
			CandidateIDs: candidateIDs,
		},
	}, nil
}

// defaultReadBudget is read_range's page in tokens when none is asked: the
// 20,000 bytes it used to cut out of the middle of a large file.
const defaultReadBudget = 5000

// ReadRange reads a line range a page at a time: whole lines up to the budget,
// and a continue handle for the rest instead of a silent cut.
func (s *Server) ReadRange(req protocol.ReadRangeRequest) (protocol.InspectResponse, error) {
	budget := req.Budget
	if handle := strings.TrimSpace(req.Continue); handle != "" {
		entry, err := s.resume(handle, "read_range")
		if err != nil {
			return protocol.InspectResponse{}, err
		}
		req.Path, req.StartLine, req.EndLine = entry.path, entry.shown, entry.through
		if budget <= 0 {
			budget = entry.budget
		}
	}
	if budget <= 0 {
		budget = defaultReadBudget
	}
	if budget > maxBudgetTokens {
		budget = maxBudgetTokens
	}

	freshness := s.workspace.Freshness(req.IndexedCommit)
	index, path, err := s.indexFor(req.Path)
	if err != nil {
		return protocol.InspectResponse{}, err
	}
	read, err := index.ReadRangePage(path, req.StartLine, req.EndLine, budget*4)
	if err != nil {
		return protocol.InspectResponse{}, withDependencyHint(req.Path, err)
	}

	continueHandle, rangeNote := "", clampNote(read.Start, read.End, read.Total, read.ClampedEnd)
	if read.NextLine > 0 {
		continueHandle = s.continuations.put(continuation{tool: "read_range", revision: s.workspace.Revision(), budget: budget, path: req.Path, shown: read.NextLine, through: read.Through})
		rangeNote = fmt.Sprintf("lines %d-%d of %d", read.Start, read.End, read.Total)
	}

	return protocol.InspectResponse{
		Digest:   s.fileDigest(req.Path),
		Continue: continueHandle,
		Revision: s.workspace.Revision(),
		Source:   read.Source,
		Range:    rangeNote,
		Freshness: protocol.Freshness{
			IndexedCommit: freshness.IndexedCommit,
			HeadCommit:    freshness.HeadCommit,
			Drifted:       freshness.Drifted,
			ChangedPaths:  freshness.ChangedPaths,
			DirtyPaths:    freshness.DirtyPaths,
			Unknown:       freshness.Unknown,
		},
	}, nil
}

// referencesAll answers references whole. References pages it by budget.
func (s *Server) referencesAll(req protocol.ReferencesRequest) (protocol.ReferencesResponse, error) {
	symbolID, err := s.resolveSymbolID(req.Path, req.SymbolID, req.SymbolName)
	if err != nil {
		return protocol.ReferencesResponse{}, err
	}
	return s.index.References(req.Path, symbolID)
}

func (s *Server) Rename(req protocol.RenameRequest) (protocol.EditResponse, error) {
	if req.NewName == "" {
		return protocol.EditResponse{}, fmt.Errorf("new name is required")
	}
	symbolID, err := s.resolveSymbolID(req.Path, req.SymbolID, req.SymbolName)
	if err != nil {
		return protocol.EditResponse{}, err
	}
	return s.edit.Rename(req.Path, symbolID, req.NewName, req.ExpectedRevision)
}

// resolveSymbolID accepts either an exact symbol ID or a name, reporting
// ambiguity with the candidate IDs so the caller can retry with one of
// them rather than having ARNO pick arbitrarily.
func (s *Server) resolveSymbolID(path string, symbolID string, symbolName string) (string, error) {
	if symbolID != "" {
		return symbolID, nil
	}
	if symbolName == "" {
		return "", fmt.Errorf("symbol id or symbol name is required")
	}

	candidates, err := s.index.SymbolsByName(path, symbolName)
	if err != nil {
		return "", err
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("symbol not found: %s", symbolName)
	}
	if len(candidates) > 1 {
		ids := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			ids = append(ids, candidate.ID)
		}
		return "", fmt.Errorf("ambiguous symbol %s: %s", symbolName, strings.Join(ids, ", "))
	}
	return candidates[0].ID, nil
}

// maxBudgetEntries bounds how many entries a budgeted tree collects to page.
const maxBudgetEntries = 20000

// WorkspaceTree lists the workspace, paged at whole entries when a budget is
// given or a handle continued.
func (s *Server) WorkspaceTree(req protocol.WorkspaceTreeRequest) (protocol.WorkspaceTreeResponse, error) {
	budget := req.Budget
	var lines []string
	offset := 0
	if handle := strings.TrimSpace(req.Continue); handle != "" {
		entry, err := s.resume(handle, "workspace_tree")
		if err != nil {
			return protocol.WorkspaceTreeResponse{}, err
		}
		lines, offset = entry.text, entry.shown
		if budget <= 0 {
			budget = entry.budget
		}
	} else {
		if budget <= 0 {
			return s.index.WorkspaceTree(req.MaxEntries)
		}
		all, err := s.index.WorkspaceTree(maxBudgetEntries)
		if err != nil {
			return all, err
		}
		for _, entry := range all.Entries {
			if entry.IsDir {
				lines = append(lines, entry.Path+"/")
			} else {
				lines = append(lines, entry.Path)
			}
		}
	}
	if budget > maxBudgetTokens {
		budget = maxBudgetTokens
	}
	if offset > len(lines) {
		offset = len(lines)
	}

	end := linePage(lines, offset, budget*4)
	response := protocol.WorkspaceTreeResponse{}
	for _, line := range lines[offset:end] {
		response.Entries = append(response.Entries, protocol.WorkspaceTreeEntry{Path: strings.TrimSuffix(line, "/"), IsDir: strings.HasSuffix(line, "/")})
	}
	switch {
	case end < len(lines):
		response.Continue = s.continuations.put(continuation{tool: "workspace_tree", revision: s.workspace.Revision(), budget: budget, text: lines, shown: end})
		response.Page = fmt.Sprintf("entries %d-%d of %d · continue=%s", offset+1, end, len(lines), response.Continue)
	case offset > 0:
		response.Page = fmt.Sprintf("entries %d-%d of %d, the last", offset+1, end, len(lines))
	}
	return response, nil
}

func (s *Server) RepositoryMap(req protocol.RepositoryMapRequest) (protocol.RepositoryMapResponse, error) {
	return s.index.RepositoryMap(req.Query, req.MaxTokens)
}

// findAll answers a find whole, up to its limit. Find pages it by budget.
func (s *Server) findAll(req protocol.FindRequest) (protocol.FindResponse, error) {
	if strings.TrimSpace(req.Dependency) == "" {
		return s.index.FindSymbols(req.Query, req.Kind, req.Limit, req.MaxLines)
	}
	index, source, err := s.dependencyIndex(strings.TrimSpace(req.Dependency))
	if err != nil {
		return protocol.FindResponse{}, err
	}
	response, err := index.FindSymbols(req.Query, req.Kind, req.Limit, req.MaxLines)
	if err != nil {
		return response, err
	}
	prefix := DependencyPrefix + source.Name + "/"
	for i := range response.Results {
		response.Results[i].Path = prefix + response.Results[i].Path
		response.Results[i].SymbolID = prefix + response.Results[i].SymbolID
	}
	response.Summary = dependencyLabel(source) + ": " + response.Summary
	return response, nil
}

func (s *Server) Search(req protocol.SearchRequest) (protocol.SearchResponse, error) {
	return s.index.Search(req.Query, req.Mode, req.Limit)
}

// Telemetry returns the recorded tool-usage summary, or clears the log.
//
// Exposed as a tool rather than left as a file on disk because telemetry
// nobody reads is telemetry nobody acts on — and the agent whose behaviour it
// measures is the one best placed to notice a rising fallback count.
func (s *Server) Telemetry(req protocol.TelemetryRequest) (protocol.TelemetryResponse, error) {
	recorder := telemetry.New(s.workspace.Root())

	if req.Reset {
		if err := recorder.Reset(); err != nil {
			return protocol.TelemetryResponse{}, err
		}
		return protocol.TelemetryResponse{
			Path:    recorder.DisplayPath(),
			Cleared: true,
			Summary: "telemetry log cleared",
		}, nil
	}

	summary, err := recorder.Summarize()
	if err != nil {
		return protocol.TelemetryResponse{}, err
	}

	response := protocol.TelemetryResponse{
		TotalCalls:  summary.TotalCalls,
		TotalErrors: summary.TotalErrors,
		TotalBytes:  summary.TotalBytes,
		Path:        summary.Path,
		Summary:     telemetry.SummaryLine(summary),
	}
	for _, stats := range summary.Tools {
		entry := protocol.TelemetryToolStats{
			Tool:   stats.Tool,
			Calls:  stats.Calls,
			Errors: stats.Errors,
			Bytes:  stats.Bytes,
			MaxMS:  stats.MaxMS,
		}
		if stats.Calls > 0 {
			entry.AvgMS = stats.TotalMS / int64(stats.Calls)
		}
		for _, failure := range stats.Failures {
			entry.Failures = append(entry.Failures, protocol.TelemetryFailure{
				Outcome: string(failure.Outcome), Count: failure.Count,
			})
		}
		response.Tools = append(response.Tools, entry)
	}
	if records, err := recorder.Read(); err == nil {
		confusion := telemetry.AnalyzeConfusion(records, req.Catalog)
		for _, change := range confusion.Switches {
			response.Switches = append(response.Switches, protocol.TelemetrySwitch{From: change.From, To: change.To, Count: change.Count})
		}
		for _, retry := range confusion.Retries {
			response.Retries = append(response.Retries, protocol.TelemetryRetry{Tool: retry.Tool, After: string(retry.After), Count: retry.Count})
		}
		response.NeverCalled = confusion.NeverCalled
	}
	for _, failure := range summary.Fallbacks {
		response.Fallbacks = append(response.Fallbacks, protocol.TelemetryFailure{
			Outcome: string(failure.Outcome), Count: failure.Count,
		})
	}
	return response, nil
}

// Grep is text search, as opposed to Search's symbol-name ranking. See
// Index.Grep for why both exist.
// grepAll answers a grep whole, up to its limit. Grep pages it by budget.
func (s *Server) grepAll(req protocol.GrepRequest) (protocol.GrepResponse, error) {
	if strings.TrimSpace(req.Dependency) == "" {
		return s.index.Grep(req)
	}
	index, source, err := s.dependencyIndex(strings.TrimSpace(req.Dependency))
	if err != nil {
		return protocol.GrepResponse{}, err
	}
	response, err := index.Grep(req)
	if err != nil {
		return response, err
	}
	prefix := DependencyPrefix + source.Name + "/"
	for i := range response.Matches {
		response.Matches[i].Path = prefix + response.Matches[i].Path
	}
	response.Summary = dependencyLabel(source) + ": " + response.Summary
	return response, nil
}

func (s *Server) SearchNudge(req protocol.SearchNudgeRequest) protocol.SearchNudgeResponse {
	footer, ok := s.index.SearchNudge(req.Command, req.OutputLength, req.FirstInSession)
	return protocol.SearchNudgeResponse{Footer: footer, Nudged: ok}
}

func (s *Server) Retrieve(req protocol.RetrievalRequest) (protocol.RetrievalResponse, error) {
	if req.MaxTokens <= 0 {
		req.MaxTokens = 2048
	}
	return s.index.Retrieve(req.Query, req.MaxTokens)
}

func (s *Server) ReplaceSymbol(req protocol.ReplaceSymbolRequest) (protocol.EditResponse, error) {
	if req.SymbolID == "" {
		return protocol.EditResponse{}, fmt.Errorf("symbol id is required")
	}
	return s.edit.ReplaceSymbol(req.SymbolID, req.ExpectedRevision, req.NewCode)
}

func (s *Server) ReplaceRange(req protocol.ReplaceRangeRequest) (protocol.EditResponse, error) {
	return s.edit.ReplaceRange(req.Path, req.ExpectedRevision, req.StartLine, req.EndLine, req.NewCode)
}

func (s *Server) Insert(req protocol.InsertRequest) (protocol.EditResponse, error) {
	if err := s.checkDigest(req.Path, req.ExpectedDigest); err != nil {
		return protocol.EditResponse{}, err
	}
	return s.edit.Insert(req.Path, req.ExpectedRevision, req.Anchor, req.Position, req.Text)
}

func (s *Server) ReplaceText(req protocol.ReplaceTextRequest) (protocol.EditResponse, error) {
	if err := s.checkDigest(req.Path, req.ExpectedDigest); err != nil {
		return protocol.EditResponse{}, err
	}
	return s.edit.ReplaceText(req.Path, req.ExpectedRevision, req.OldText, req.NewText)
}

func (s *Server) CreateFile(req protocol.CreateFileRequest) (protocol.EditResponse, error) {
	return s.edit.CreateFile(req.Path, req.Content)
}

// ReplaceFile overwrites an existing file wholesale — the case no anchored
// edit can express, since there is nothing to anchor on when a document is
// being rewritten rather than amended.
func (s *Server) ReplaceFile(req protocol.ReplaceFileRequest) (protocol.EditResponse, error) {
	return s.edit.ReplaceFile(req.Path, req.Content)
}

func (s *Server) Apply(req protocol.ApplyRequest) (protocol.ApplyResponse, error) {
	// Every digest is checked before any edit is written.
	for index, op := range req.Edits {
		if err := s.checkDigest(op.Path, op.ExpectedDigest); err != nil {
			return protocol.ApplyResponse{}, fmt.Errorf("edit %d (%s %s): %w", index+1, op.Op, op.Path, err)
		}
	}
	return s.edit.Apply(req)
}

func (s *Server) DeleteSymbol(req protocol.DeleteSymbolRequest) (protocol.EditResponse, error) {
	symbolID, err := s.resolveSymbolID(req.Path, req.SymbolID, req.SymbolName)
	if err != nil {
		return protocol.EditResponse{}, err
	}
	return s.edit.DeleteSymbol(req.Path, symbolID, req.ExpectedRevision)
}

func (s *Server) DeleteFile(req protocol.DeleteFileRequest) (protocol.EditResponse, error) {
	return s.edit.DeleteFile(req.Path)
}

func (s *Server) RunTests(req protocol.RunTestsRequest) protocol.RunTestsResponse {
	scope := jobs.TestScope{Kind: req.Scope, File: req.File, Test: req.Test}
	key := "tests|" + req.Scope + "|" + req.File + "|" + req.Test + "|" + s.workspace.Revision()
	jobID, joined := s.jobs.StartOnce("tests", key)
	if !joined {
		s.jobs.RunScopedTests(jobID, s.workspace.Root(), scope, s.workspace.Changes())
	}

	if !req.Wait {
		return protocol.RunTestsResponse{JobID: jobID, Outcome: protocol.OutcomeRunning, Status: "running"}
	}

	// Reuses 12.2's Runner.Wait rather than reimplementing the block, so
	// check() and run_tests() cannot drift in how they treat a timeout.
	output, finished := s.jobs.Wait(jobID, checkTimeout(req.TimeoutSeconds))
	if !finished {
		return protocol.RunTestsResponse{
			JobID:   jobID,
			Outcome: protocol.OutcomeTimedOut,
			Status:  "running",
			Summary: stillRunning("tests", "run_tests", checkTimeout(req.TimeoutSeconds), jobID),
		}
	}

	outcome := finishedOutcome(output)
	runKind := "run_tests"
	if req.Scope != "" {
		runKind += " " + req.Scope
	}
	// A narrowed run that ran nothing exits 0 in cargo and go test, and read
	// as a pass for a test name that matched no test.
	if outcome == protocol.OutcomePassed && req.Scope != "" && req.Scope != "all" && jobs.NoTestsRan(output.Raw) {
		s.workspace.RecordRun(runKind, string(protocol.OutcomeFailed))
		return protocol.RunTestsResponse{
			JobID:   jobID,
			Outcome: protocol.OutcomeFailed,
			Status:  output.Status,
			Summary: fmt.Sprintf("no test matched %q in scope %s — nothing ran", req.Test+req.File, req.Scope),
		}
	}
	runPassed := outcome == protocol.OutcomePassed
	s.workspace.RecordRun(runKind, string(outcome))
	return protocol.RunTestsResponse{
		JobID:   jobID,
		Outcome: outcome,
		Status:  output.Status,
		Passed:  runPassed,
		Summary: verdictSummary(runPassed, output.Summary, output.Raw),
	}
}

// maxSymbolDeltaFiles bounds how many changed files get a symbol-level delta.
//
// Computing one means parsing both the committed and working copy of every
// changed file. On a branch with 88 changed files that measured at 704ms and
// 4.6KB — roughly 35x the latency and 5x the bytes of any other ARNO call —
// which telemetry surfaced on its first live session and sixteen tasks of
// hand-written feedback never noticed.
//
// The cutoff is not only about cost. Past this many files the renderer's own
// 40-symbol cap turns the result into an arbitrary sample — 40 of 683 changes,
// chosen by file order — and an arbitrary sample presented as a summary is
// worse than no summary, because it reads like the whole answer. Below the
// cutoff the delta is the useful thing it was built to be.
const maxSymbolDeltaFiles = 25

func (s *Server) Changes() protocol.ChangesResponse {
	response := s.workspace.ChangesResponse()

	if len(response.Files) > maxSymbolDeltaFiles {
		// Degrades automatically rather than behind a flag: the pathological
		// case is exactly the one where a caller is least likely to know to
		// ask for the cheap variant. It must say so, though — silently
		// returning less than the response shape implies would be worse than
		// the cost it avoids.
		response.SymbolsOmitted = len(response.Files)
		return response
	}

	response.Symbols = s.changedSymbols(response.Files)
	return response
}

// changedSymbols computes the symbol-level delta for every changed file.
// This is the composition seam 9.1 needs: the workspace knows what git says
// changed and can produce the committed bytes, the index knows how to parse
// symbols, and neither package has to learn about the other.
func (s *Server) changedSymbols(files []protocol.ChangedFile) []protocol.SymbolChange {
	changes := make([]protocol.SymbolChange, 0)

	for _, file := range files {
		// Absent at HEAD means newly created, which is a real answer (every
		// symbol is added), not a failure — so the nil old side is passed
		// through rather than skipped.
		oldSource, _ := s.workspace.FileAtHead(file.Path)
		newSource, err := os.ReadFile(filepath.Join(s.workspace.Root(), file.Path))
		if err != nil {
			// Deleted since git reported it, or unreadable. The file-level
			// counts still stand; only the symbol detail is unavailable.
			newSource = nil
		}
		changes = append(changes, s.index.SymbolDelta(file.Path, oldSource, newSource)...)
	}
	return changes
}

// defaultDiffBudget is diff's page in tokens when none is asked: the 8,000
// bytes it used to cut out of the middle of a large patch.
const defaultDiffBudget = 2000

// Diff returns a patch a page at a time: whole lines up to the budget, and a
// continue handle for the rest instead of a cut middle.
func (s *Server) Diff(req protocol.DiffRequest) (protocol.DiffResponse, error) {
	budget := req.Budget
	if handle := strings.TrimSpace(req.Continue); handle != "" {
		entry, err := s.resume(handle, "diff")
		if err != nil {
			return protocol.DiffResponse{}, err
		}
		if budget <= 0 {
			budget = entry.budget
		}
		return s.pageDiff(entry.path, entry.summary, entry.text, entry.shown, budget), nil
	}
	if budget <= 0 {
		budget = defaultDiffBudget
	}
	patch, summary, err := s.workspace.DiffPatch(req.Target, req.Since)
	if err != nil {
		return protocol.DiffResponse{}, err
	}
	return s.pageDiff(strings.TrimSpace(req.Target), summary, strings.Split(patch, "\n"), 0, budget), nil
}

func (s *Server) pageDiff(target string, summary string, lines []string, offset int, budget int) protocol.DiffResponse {
	if budget > maxBudgetTokens {
		budget = maxBudgetTokens
	}
	if offset > len(lines) {
		offset = len(lines)
	}
	end := linePage(lines, offset, budget*4)
	response := protocol.DiffResponse{Target: target, Patch: strings.Join(lines[offset:end], "\n"), Summary: summary}
	switch {
	case end < len(lines):
		response.Continue = s.continuations.put(continuation{tool: "diff", revision: s.workspace.Revision(), budget: budget, path: target, summary: summary, text: lines, shown: end})
		response.Summary = fmt.Sprintf("%s · patch lines %d-%d of %d · continue=%s", summary, offset+1, end, len(lines), response.Continue)
	case offset > 0:
		response.Summary = fmt.Sprintf("%s · patch lines %d-%d of %d, the last", summary, offset+1, end, len(lines))
	}
	return response
}

func (s *Server) Checkpoint(req protocol.CheckpointRequest) protocol.CheckpointResponse {
	checkpoint := s.workspace.Checkpoint(req.Note)
	return protocol.CheckpointResponse{
		ID:       checkpoint.ID,
		Note:     checkpoint.Note,
		Revision: checkpoint.Revision,
		Paths:    checkpoint.Paths,
		Head:     checkpoint.Head,
	}
}

func (s *Server) Revert(req protocol.RevertRequest) (protocol.CheckpointResponse, error) {
	if req.CheckpointID == "" {
		return protocol.CheckpointResponse{}, fmt.Errorf("checkpoint id is required")
	}

	checkpoint, err := s.workspace.RevertCheckpoint(req.CheckpointID)
	if err != nil {
		return protocol.CheckpointResponse{}, err
	}

	return protocol.CheckpointResponse{
		ID:       checkpoint.ID,
		Note:     checkpoint.Note,
		Revision: checkpoint.Revision,
		Paths:    checkpoint.Paths,
		Head:     checkpoint.Head,
	}, nil
}

func (s *Server) JobStatus(id string) (protocol.JobStatusResponse, error) {
	kind, status, summary, ok := s.jobs.Status(id)
	if !ok {
		return protocol.JobStatusResponse{}, fmt.Errorf("job not found: %s", id)
	}
	return protocol.JobStatusResponse{
		ID:      id,
		Kind:    kind,
		Status:  status,
		Summary: summary,
	}, nil
}

func (s *Server) JobOutput(id string) (protocol.JobOutputResponse, error) {
	output, ok := s.jobs.Output(id)
	if !ok {
		return protocol.JobOutputResponse{}, fmt.Errorf("job not found: %s", id)
	}

	return protocol.JobOutputResponse{
		ID:           id,
		Kind:         output.Kind,
		Status:       output.Status,
		Summary:      output.Summary,
		RawOutput:    output.Raw,
		OmittedBytes: output.OmittedBytes,
	}, nil
}

// JobOutputPage returns a job's output a page at a time: whole lines up to the
// budget and a continue handle for the rest, instead of the clamped middle.
func (s *Server) JobOutputPage(id string, budget int, handle string) (protocol.JobOutputResponse, error) {
	offset := 0
	if handle = strings.TrimSpace(handle); handle != "" {
		entry, err := s.resume(handle, "job_output")
		if err != nil {
			return protocol.JobOutputResponse{}, err
		}
		id, offset = entry.path, entry.shown
		if budget <= 0 {
			budget = entry.budget
		}
	}
	if budget <= 0 {
		budget = defaultDiffBudget
	}
	if budget > maxBudgetTokens {
		budget = maxBudgetTokens
	}

	response, err := s.JobOutput(id)
	if err != nil {
		return response, err
	}
	full, ok := s.jobs.FullOutput(id)
	if !ok || full == "" {
		return response, nil
	}
	lines := strings.Split(full, "\n")
	if offset > len(lines) {
		offset = len(lines)
	}
	end := linePage(lines, offset, budget*4)
	response.RawOutput = strings.Join(lines[offset:end], "\n")
	response.OmittedBytes = 0
	switch {
	case end < len(lines):
		response.Continue = s.continuations.put(continuation{tool: "job_output", revision: s.workspace.Revision(), budget: budget, path: id, shown: end})
		response.Lines = fmt.Sprintf("lines %d-%d of %d · continue=%s", offset+1, end, len(lines), response.Continue)
	case offset > 0:
		response.Lines = fmt.Sprintf("lines %d-%d of %d, the last", offset+1, end, len(lines))
	}
	return response, nil
}

func (s *Server) Events(after int64, limit int) protocol.EventsResponse {
	if s.bus == nil {
		return protocol.EventsResponse{}
	}

	records, latest := s.bus.Events(after, limit)
	converted := make([]protocol.EventRecord, 0, len(records))
	for _, record := range records {
		converted = append(converted, protocol.EventRecord{
			Cursor: record.Cursor,
			Event: protocol.Event{
				Type:    record.Event.Type,
				Entity:  record.Event.Entity,
				Payload: record.Event.Payload,
			},
		})
	}

	return protocol.EventsResponse{Cursor: latest, Events: converted}
}

package edit

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/julianbei/jade/internal/jobs"
	"github.com/julianbei/jade/internal/pathguard"
	"github.com/julianbei/jade/internal/protocol"
)

// Apply performs several edits as one unit: all of them land, or none do.
//
// jade edits one site per call, and adding a single tool to this repository
// touches six files. That made every such task cost six calls and six
// revision round trips, so the work got scripted in `python3` instead —
// bypassing revision checks entirely. That fallback caused the only outage
// of the dogfooding session: a scripted replace hit the wrong anchor, left a
// duplicate import, and broke the MCP server's own build.
//
// Atomicity here is rollback-based rather than write-to-temp: every touched
// file is snapshotted before the first write and restored if any later step
// fails. That reuses the existing single-edit primitives unchanged, which
// matters more than avoiding a brief window of partial state in a
// single-process tool.
func (s *Service) Apply(req protocol.ApplyRequest) (protocol.ApplyResponse, error) {
	if len(req.Edits) == 0 {
		return protocol.ApplyResponse{}, fmt.Errorf("no edits given")
	}

	current := s.workspace.Revision()
	if req.ExpectedRevision != "" && req.ExpectedRevision != current {
		return protocol.ApplyResponse{}, ErrStaleRevision
	}

	root := s.workspace.Root()
	if err := s.preflight(root, req.Edits); err != nil {
		return protocol.ApplyResponse{}, err
	}

	snapshots, err := snapshotFiles(root, req.Edits)
	if err != nil {
		return protocol.ApplyResponse{}, err
	}

	response := protocol.ApplyResponse{}
	touched := make(map[string]bool)
	written := make([]protocol.EditOp, 0, len(req.Edits))

	for index, op := range req.Edits {
		added, removed, err := s.applyOne(op)
		if err != nil {
			restoreFiles(root, snapshots)
			return protocol.ApplyResponse{}, fmt.Errorf("edit %d (%s %s) failed, all %d edits rolled back: %w",
				index+1, op.Op, op.Path, len(req.Edits), err)
		}
		response.Applied++
		response.AddedLines += added
		response.RemovedLines += removed
		touched[op.Path] = true
		if op.NewText != "" && op.Op != "replace_symbol" {
			written = append(written, op)
		}
	}

	response.Changed = sortedPaths(touched)

	if req.Format {
		response.Formatted = formatFiles(root, response.Changed)
	}

	oldRev, newRev := s.workspace.BumpRevision(response.Changed...)
	response.OldRevision = oldRev
	response.NewRevision = newRev

	for _, op := range written {
		if len(response.Snippets) >= maxSnippets {
			break
		}
		response.Snippets = append(response.Snippets, snippets(root, op.Path, op.NewText)...)
	}

	for _, path := range response.Changed {
		response.Diagnostics = append(response.Diagnostics, s.diag.Immediate(path)...)
	}
	response.Checks = s.diag.Checks(response.Changed...)

	if kind := strings.TrimSpace(req.Check); kind != "" {
		response.CheckOutcome, response.CheckSummary = s.runCheck(kind)
		response.CheckPassed = response.CheckOutcome == protocol.OutcomePassed
		response.CheckStatus = string(response.CheckOutcome)
	}

	response.Summary = applySummary(response, req.Check)
	return response, nil
}

// preflight validates what can be known before anything is written: that
// every op is well formed and its file exists, and that anchors resolve
// uniquely.
//
// Anchor checking is skipped for a file an earlier op in the same batch
// already targets, because that op may be what creates the anchor. Rollback
// covers the rest. Checking what is checkable still catches the common
// mistake — a typo'd or ambiguous anchor across distinct files — before any
// state changes at all.
func (s *Service) preflight(root string, edits []protocol.EditOp) error {
	willTouch := make(map[string]bool)

	for index, op := range edits {
		if strings.TrimSpace(op.Path) == "" {
			return fmt.Errorf("edit %d: path is required", index+1)
		}
		if err := validateOpShape(index, op); err != nil {
			return err
		}

		// Checked for every op, including repeats of a path, before anything
		// is written: one escaping op refuses the whole batch.
		absolute, err := pathguard.Resolve(root, op.Path)
		if err != nil {
			return fmt.Errorf("edit %d: %w", index+1, err)
		}

		if !willTouch[op.Path] {
			data, err := os.ReadFile(absolute)
			if err != nil {
				return fmt.Errorf("edit %d (%s): %w", index+1, op.Path, err)
			}
			if err := validateAnchor(index, op, string(data)); err != nil {
				return err
			}
		}
		willTouch[op.Path] = true
	}
	return nil
}

func validateOpShape(index int, op protocol.EditOp) error {
	switch op.Op {
	case "replace_text":
		if op.OldText == "" {
			return fmt.Errorf("edit %d (replace_text): oldText is required", index+1)
		}
	case "replace_range":
		if op.StartLine < 1 || op.EndLine < op.StartLine {
			return fmt.Errorf("edit %d (replace_range): invalid range %d-%d", index+1, op.StartLine, op.EndLine)
		}
	case "replace_symbol", "delete_symbol":
		if op.SymbolID == "" && op.SymbolName == "" {
			return fmt.Errorf("edit %d (%s): symbolId or symbolName is required", index+1, op.Op)
		}
	case "insert":
		if op.NewText == "" {
			return fmt.Errorf("edit %d (insert): newText is required", index+1)
		}
	default:
		return fmt.Errorf("edit %d: unknown op %q", index+1, op.Op)
	}
	return nil
}

// validateAnchor enforces replace_text's uniqueness rule ahead of time, so a
// batch with an ambiguous anchor fails before writing rather than halfway
// through and rolling back.
func validateAnchor(index int, op protocol.EditOp, source string) error {
	anchor := ""
	switch op.Op {
	case "replace_text":
		anchor = op.OldText
	case "insert":
		anchor = op.Anchor
	}
	if anchor == "" {
		return nil
	}

	switch strings.Count(source, anchor) {
	case 0:
		return fmt.Errorf("edit %d (%s %s): anchor text not found", index+1, op.Op, op.Path)
	case 1:
		return nil
	default:
		return fmt.Errorf("edit %d (%s %s): anchor is ambiguous, %d matches — extend it with surrounding context",
			index+1, op.Op, op.Path, strings.Count(source, anchor))
	}
}

// applyOne runs a single op through the existing primitives. It returns
// added and removed line counts; no revision is bumped and no validation job
// is started, because Apply does both once for the whole unit.
func (s *Service) applyOne(op protocol.EditOp) (int, int, error) {
	switch op.Op {
	case "replace_text":
		_, err := s.index.ReplaceTextSource(op.Path, op.OldText, op.NewText)
		added, removed := lineDelta(op.OldText, op.NewText)
		return added, removed, err

	case "replace_range":
		oldLines, err := s.index.ReplaceRangeSource(op.Path, op.StartLine, op.EndLine, op.NewText)
		return countLines(op.NewText), len(oldLines), err

	case "replace_symbol":
		symbolID, err := s.resolveSymbol(op)
		if err != nil {
			return 0, 0, err
		}
		_, oldLines, err := s.index.ReplaceSymbolSource(symbolID, op.NewText)
		return countLines(op.NewText), len(oldLines), err

	case "delete_symbol":
		symbolID, err := s.resolveSymbol(op)
		if err != nil {
			return 0, 0, err
		}
		_, removed, err := s.index.DeleteSymbolSource(op.Path, symbolID)
		return 0, len(removed), err

	case "insert":
		added, err := s.index.InsertSource(op.Path, op.Anchor, op.Position, op.NewText)
		return added, 0, err

	default:
		return 0, 0, fmt.Errorf("unknown op %q", op.Op)
	}
}

// resolveSymbol accepts an id or a name, reporting ambiguity with candidates
// rather than picking one.
func (s *Service) resolveSymbol(op protocol.EditOp) (string, error) {
	if op.SymbolID != "" {
		return op.SymbolID, nil
	}

	candidates, err := s.index.SymbolsByName(op.Path, op.SymbolName)
	if err != nil {
		return "", err
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("symbol not found: %s", op.SymbolName)
	}
	if len(candidates) > 1 {
		ids := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			ids = append(ids, candidate.ID)
		}
		return "", fmt.Errorf("ambiguous symbol %s: %s", op.SymbolName, strings.Join(ids, ", "))
	}
	return candidates[0].ID, nil
}

// applyCheckTimeout bounds the single post-batch validation.
const applyCheckTimeout = 120 * time.Second

// runCheck runs one validation for the whole batch instead of one per edit.
// A batch of six edits previously started six typecheck jobs, five of which
// were racing against files that were still mid-change.
func (s *Service) runCheck(kind string) (protocol.ValidationOutcome, string) {
	switch kind {
	case "build", "typecheck", "tests":
	default:
		return "", fmt.Sprintf("unknown check kind %q", kind)
	}

	jobID := s.jobs.Start(kind)
	s.jobs.RunValidationCommand(jobID, s.workspace.Root(), kind)

	output, finished := s.jobs.Wait(jobID, applyCheckTimeout)
	if !finished {
		return protocol.OutcomeTimedOut, fmt.Sprintf("%s did not finish within %s — poll job_status %s", kind, applyCheckTimeout, jobID)
	}
	outcome := jobs.Outcome(output, true)
	if outcome == protocol.OutcomePassed {
		// The verdict is already in the response header. A passing suite's
		// summary is whatever it logged, and cobra's logs expected errors:
		// shown under "pass tests", they sent the agent to re-run every test.
		return outcome, ""
	}
	return outcome, output.Summary
}

// checkOutputPassed reads the verdict from the command's own output rather
// than from the job having completed — a failing run also completes.
func checkOutputPassed(summary string, raw string) bool {
	combined := strings.ToLower(summary + "\n" + raw)
	for _, marker := range []string{"fail", "error", "cannot find", "undefined:", "panic:"} {
		if strings.Contains(combined, marker) {
			return false
		}
	}
	return true
}

func snapshotFiles(root string, edits []protocol.EditOp) (map[string][]byte, error) {
	snapshots := make(map[string][]byte)
	for _, op := range edits {
		if _, taken := snapshots[op.Path]; taken {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, op.Path))
		if err != nil {
			return nil, fmt.Errorf("cannot snapshot %s for rollback: %w", op.Path, err)
		}
		snapshots[op.Path] = data
	}
	return snapshots, nil
}

// restoreFiles puts every snapshotted file back. A restore failure is
// deliberately not surfaced over the original error: the caller needs to
// know which edit failed, and a half-restored tree is visible through
// changes() anyway.
func restoreFiles(root string, snapshots map[string][]byte) {
	for path, data := range snapshots {
		_ = os.WriteFile(filepath.Join(root, path), data, 0o644)
	}
}

func sortedPaths(set map[string]bool) []string {
	paths := make([]string, 0, len(set))
	for path := range set {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func applySummary(r protocol.ApplyResponse, check string) string {
	parts := []string{fmt.Sprintf("%d edits across %d files, +%d -%d",
		r.Applied, len(r.Changed), r.AddedLines, r.RemovedLines)}

	if len(r.Formatted) > 0 {
		parts = append(parts, fmt.Sprintf("%d formatted", len(r.Formatted)))
	}
	if len(r.Diagnostics) > 0 {
		parts = append(parts, fmt.Sprintf("%d diagnostics", len(r.Diagnostics)))
	}
	if strings.TrimSpace(check) != "" {
		verdict := string(r.CheckOutcome)
		switch r.CheckOutcome {
		case protocol.OutcomePassed:
			verdict = "pass"
		case protocol.OutcomeFailed:
			verdict = "FAIL"
		}
		parts = append(parts, verdict+" "+check)
	}
	return strings.Join(parts, " · ")
}

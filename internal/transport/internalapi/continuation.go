package internalapi

import (
	"fmt"
	"strings"
	"sync"

	"github.com/julianbei/jade/internal/protocol"
)

// Budgets and continuation (docs/tool-contract.md): a caller names a budget in
// tokens, the answer is cut at whole items, and the rest waits behind a handle
// for the same call's next page.

const (
	// maxContinuations bounds the handles kept per server; the oldest goes first.
	maxContinuations = 64
	// maxBudgetTokens bounds one page, whatever budget is asked.
	maxBudgetTokens = 20000
	// maxBudgetMatches bounds how many matches a budgeted grep collects to page
	// through. Total still counts past it.
	maxBudgetMatches = 2000
)

type continuation struct {
	// tool is the tool whose answer this continues; a handle answers only it.
	tool     string
	revision string
	budget   int
	shown    int
	grep     protocol.GrepResponse
	find     *protocol.FindResponse
	refs     *protocol.ReferencesResponse
}

type continuationStore struct {
	mu      sync.Mutex
	next    int
	entries map[string]continuation
	order   []string
}

func (c *continuationStore) put(entry continuation) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]continuation{}
	}
	c.next++
	handle := fmt.Sprintf("c%d", c.next)
	c.entries[handle] = entry
	c.order = append(c.order, handle)
	for len(c.order) > maxContinuations {
		delete(c.entries, c.order[0])
		c.order = c.order[1:]
	}
	return handle
}

func (c *continuationStore) get(handle string) (continuation, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[handle]
	return entry, ok
}

// Grep answers a grep, paged when a budget is given or a handle continued.
// A handle is never answered from a different workspace state: after an edit
// it is refused with both revisions named.
func (s *Server) Grep(req protocol.GrepRequest) (protocol.GrepResponse, error) {
	if handle := strings.TrimSpace(req.Continue); handle != "" {
		entry, err := s.resume(handle, "grep")
		if err != nil {
			return protocol.GrepResponse{}, err
		}
		budget := req.Budget
		if budget <= 0 {
			budget = entry.budget
		}
		return s.pageGrep(entry.grep, entry.shown, budget), nil
	}
	if req.Budget <= 0 {
		return s.grepAll(req)
	}
	req.Limit = maxBudgetMatches
	all, err := s.grepAll(req)
	if err != nil {
		return all, err
	}
	return s.pageGrep(all, 0, req.Budget), nil
}

// pageGrep returns the matches from offset that fit budget, whole matches and
// at least one, and keeps the rest behind a handle.
func (s *Server) pageGrep(all protocol.GrepResponse, offset int, budget int) protocol.GrepResponse {
	if budget > maxBudgetTokens {
		budget = maxBudgetTokens
	}
	if offset > len(all.Matches) {
		offset = len(all.Matches)
	}
	rest := all.Matches[offset:]
	count, used := 0, 0
	for count < len(rest) {
		cost := grepMatchTokens(rest[count])
		if count > 0 && used+cost > budget {
			break
		}
		used += cost
		count++
	}
	end := offset + count

	page := all
	page.Matches = rest[:count]
	page.Continue = ""
	switch {
	case end < len(all.Matches):
		page.Continue = s.continuations.put(continuation{tool: "grep", revision: s.workspace.Revision(), budget: budget, grep: all, shown: end})
		page.Truncated = true
		page.Provenance.Completeness = protocol.CompletenessCut
		page.Summary = fmt.Sprintf("%d matches in %d files, shown %d-%d · continue=%s · %s",
			all.Total, all.Files, offset+1, end, page.Continue, page.Provenance.String())
	case all.Total > len(all.Matches):
		page.Truncated = true
		page.Provenance.Completeness = protocol.CompletenessCut
		page.Summary = fmt.Sprintf("%d matches in %d files, shown %d-%d; only the first %d can be paged — narrow with glob/exclude · %s",
			all.Total, all.Files, offset+1, end, len(all.Matches), page.Provenance.String())
	case offset > 0:
		page.Truncated = false
		page.Provenance.Completeness = protocol.CompletenessComplete
		page.Summary = fmt.Sprintf("%d matches in %d files, shown %d-%d, the last · %s",
			all.Total, all.Files, offset+1, end, page.Provenance.String())
	}
	return page
}

// grepMatchTokens estimates a rendered match at four bytes a token, the
// estimate the benchmark uses.
func grepMatchTokens(match protocol.GrepMatch) int {
	size := len(match.Path) + len(match.Text) + 8
	for _, after := range match.After {
		size += len(after) + 5
	}
	return size/4 + 1
}

// resume returns the continuation behind handle for tool, refusing one that is
// unknown, belongs to another tool, or was cut at another revision.
func (s *Server) resume(handle string, tool string) (continuation, error) {
	entry, ok := s.continuations.get(handle)
	if !ok {
		return continuation{}, fmt.Errorf("continue=%s is unknown or expired — repeat the call", handle)
	}
	if entry.tool != tool {
		return continuation{}, fmt.Errorf("continue=%s continues %s, not %s", handle, entry.tool, tool)
	}
	if revision := s.workspace.Revision(); revision != entry.revision {
		return continuation{}, fmt.Errorf("continue=%s was cut at %s; the workspace is at %s — repeat the call", handle, entry.revision, revision)
	}
	return entry, nil
}

// maxBudgetItems bounds how many declarations or references a budgeted call
// collects to page through.
const maxBudgetItems = 500

// budgetPage returns the end of the page that starts at offset: whole items
// while they fit budget, and at least one.
func budgetPage(cost func(int) int, offset int, total int, budget int) int {
	if budget > maxBudgetTokens {
		budget = maxBudgetTokens
	}
	end, used := offset, 0
	for end < total {
		item := cost(end)
		if end > offset && used+item > budget {
			break
		}
		used += item
		end++
	}
	return end
}

// Find answers a find, paged when a budget is given or a handle continued.
func (s *Server) Find(req protocol.FindRequest) (protocol.FindResponse, error) {
	if handle := strings.TrimSpace(req.Continue); handle != "" {
		entry, err := s.resume(handle, "find")
		if err != nil {
			return protocol.FindResponse{}, err
		}
		budget := req.Budget
		if budget <= 0 {
			budget = entry.budget
		}
		return s.pageFind(*entry.find, entry.shown, budget), nil
	}
	if req.Budget <= 0 {
		return s.findAll(req)
	}
	req.Limit = maxBudgetItems
	all, err := s.findAll(req)
	if err != nil {
		return all, err
	}
	return s.pageFind(all, 0, req.Budget), nil
}

// pageFind returns the declarations from offset that fit budget, whole
// declarations with their bodies, and keeps the rest behind a handle. The
// summary carries no provenance: the renderer appends it.
func (s *Server) pageFind(all protocol.FindResponse, offset int, budget int) protocol.FindResponse {
	if offset > len(all.Results) {
		offset = len(all.Results)
	}
	end := budgetPage(func(i int) int {
		result := all.Results[i]
		return (len(result.Path)+len(result.Symbol)+len(result.Kind)+len(result.Body)+24)/4 + 1
	}, offset, len(all.Results), budget)

	page := all
	page.Results = all.Results[offset:end]
	page.Continue = ""
	switch {
	case end < len(all.Results):
		page.Continue = s.continuations.put(continuation{tool: "find", revision: s.workspace.Revision(), budget: budget, find: &all, shown: end})
		page.Provenance.Completeness = protocol.CompletenessCut
		page.Summary = fmt.Sprintf("%d matches for %q, shown %d-%d · continue=%s", all.Total, all.Query, offset+1, end, page.Continue)
	case all.Total > len(all.Results):
		page.Provenance.Completeness = protocol.CompletenessCut
		page.Summary = fmt.Sprintf("%d matches for %q, shown %d-%d; only the first %d can be paged — narrow with kind", all.Total, all.Query, offset+1, end, len(all.Results))
	case offset > 0:
		page.Summary = fmt.Sprintf("%d matches for %q, shown %d-%d, the last", all.Total, all.Query, offset+1, end)
	}
	return page
}

// References answers references, paged when a budget is given or a handle
// continued.
func (s *Server) References(req protocol.ReferencesRequest) (protocol.ReferencesResponse, error) {
	if handle := strings.TrimSpace(req.Continue); handle != "" {
		entry, err := s.resume(handle, "references")
		if err != nil {
			return protocol.ReferencesResponse{}, err
		}
		budget := req.Budget
		if budget <= 0 {
			budget = entry.budget
		}
		return s.pageReferences(*entry.refs, entry.shown, budget), nil
	}
	all, err := s.referencesAll(req)
	if err != nil || req.Budget <= 0 {
		return all, err
	}
	return s.pageReferences(all, 0, req.Budget), nil
}

// pageReferences returns the references from offset that fit budget and keeps
// the rest behind a handle. The renderer appends provenance to the summary.
func (s *Server) pageReferences(all protocol.ReferencesResponse, offset int, budget int) protocol.ReferencesResponse {
	if offset > len(all.References) {
		offset = len(all.References)
	}
	end := budgetPage(func(i int) int {
		ref := all.References[i]
		return (len(ref.Path)+len(ref.Symbol)+len(ref.Confidence)+20)/4 + 1
	}, offset, len(all.References), budget)

	page := all
	page.References = all.References[offset:end]
	page.Continue = ""
	switch {
	case end < len(all.References):
		page.Continue = s.continuations.put(continuation{tool: "references", revision: s.workspace.Revision(), budget: budget, refs: &all, shown: end})
		page.Provenance.Completeness = protocol.CompletenessCut
		page.Summary = fmt.Sprintf("%s, shown %d-%d · continue=%s", all.Summary, offset+1, end, page.Continue)
	case offset > 0:
		page.Summary = fmt.Sprintf("%s, shown %d-%d, the last", all.Summary, offset+1, end)
	}
	return page
}

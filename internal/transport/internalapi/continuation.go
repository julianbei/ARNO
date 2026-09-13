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
	revision string
	budget   int
	grep     protocol.GrepResponse
	shown    int
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
		entry, ok := s.continuations.get(handle)
		if !ok {
			return protocol.GrepResponse{}, fmt.Errorf("continue=%s is unknown or expired — repeat the call", handle)
		}
		if revision := s.workspace.Revision(); revision != entry.revision {
			return protocol.GrepResponse{}, fmt.Errorf("continue=%s was cut at %s; the workspace is at %s — repeat the call", handle, entry.revision, revision)
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
		page.Continue = s.continuations.put(continuation{revision: s.workspace.Revision(), budget: budget, grep: all, shown: end})
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

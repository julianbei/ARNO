package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/julianbei/arno/internal/pathguard"
)

// maxOpenWorkspacesDefault bounds how many worktrees one server keeps open at
// once. The bound exists because a graph is not cheap: each one holds its own
// language servers, and jdtls or rust-analyzer on a large repository is
// hundreds of megabytes and a core's worth of indexing. A fleet with sixty
// per-PR worktrees must not be able to ask one server for sixty of them.
//
// Three is the working set a session actually has — the worktree it was
// started in, the one it is editing, and one it is comparing against — and a
// fourth evicts the least recently used rather than failing the call.
const maxOpenWorkspacesDefault = 3

// envMaxWorkspaces overrides the bound for a session that genuinely needs a
// wider working set and has the memory for it.
const envMaxWorkspaces = "ARNO_MAX_WORKSPACES"

// maxOpenWorkspaces reads the bound from the environment, falling back to the
// default for an unset, unparseable or non-positive value. A bad value is not
// worth failing a server start over: the default is always a safe answer.
func maxOpenWorkspaces() int {
	raw := strings.TrimSpace(os.Getenv(envMaxWorkspaces))
	if raw == "" {
		return maxOpenWorkspacesDefault
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return maxOpenWorkspacesDefault
	}
	return n
}

// graphCache holds the workspace graphs this server has open, most recently
// used first. It is not safe for concurrent use and does not need to be: the
// MCP loop handles one request at a time, which is the same reason the
// workspace switch needs no lock.
type graphCache struct {
	max  int
	open []*workspaceGraph
}

func newGraphCache(max int) *graphCache {
	return &graphCache{max: max}
}

// lookup returns the open graph for root and marks it most recently used, or
// nil when this server has no graph for that directory. Roots are compared
// resolved, so a symlinked spelling finds the graph it names.
func (c *graphCache) lookup(root string) *workspaceGraph {
	for i, g := range c.open {
		if sameWorkspaceDir(g.root, root) {
			c.touch(i)
			return g
		}
	}
	return nil
}

// touch moves the graph at i to the front of the list.
func (c *graphCache) touch(i int) {
	if i <= 0 {
		return
	}
	g := c.open[i]
	copy(c.open[1:i+1], c.open[:i])
	c.open[0] = g
}

// add records a newly opened graph as the most recently used one.
func (c *graphCache) add(g *workspaceGraph) {
	c.open = append([]*workspaceGraph{g}, c.open...)
}

// evict drops least recently used graphs until the cache is within its bound
// and returns the ones removed, for the caller to close. keep is never
// evicted however old it is: it is the session's default workspace, the one
// every call that names no root lands in, so evicting it would make the
// cheapest call the one that pays to rebuild.
func (c *graphCache) evict(keep *workspaceGraph) []*workspaceGraph {
	var dropped []*workspaceGraph
	for len(c.open) > c.max {
		victim := -1
		for i := len(c.open) - 1; i >= 0; i-- {
			if c.open[i] != keep {
				victim = i
				break
			}
		}
		// Only the default is left: it is kept, so the cache stays one over
		// its bound rather than closing the workspace the session is serving.
		if victim < 0 {
			break
		}
		dropped = append(dropped, c.open[victim])
		c.open = append(c.open[:victim], c.open[victim+1:]...)
	}
	return dropped
}

// roots lists the open workspaces, most recently used first.
func (c *graphCache) roots() []string {
	list := make([]string, 0, len(c.open))
	for _, g := range c.open {
		list = append(list, g.root)
	}
	return list
}

// closeAll releases every open graph. It runs at shutdown, where closing the
// language servers in sequence is the point rather than a cost.
func (c *graphCache) closeAll() {
	for _, g := range c.open {
		g.close()
	}
	c.open = nil
}

// graphForRoot returns the graph serving target, opening it if this server has
// not served it yet. target must be a worktree of the repository the server
// was started in — the same containment rule the workspace tool enforces, and
// the reason a root argument cannot be used to reach an unrelated directory.
func (s *mcpServer) graphForRoot(ctx context.Context, target string) (*workspaceGraph, error) {
	// A cached root was validated when it was opened, so a hit needs no second
	// trip to git. This is also what makes a subagent's repeated calls into its
	// own worktree cost the same as calls into the default one.
	if hit := s.graphs.lookup(target); hit != nil {
		return hit, nil
	}

	list, err := gitWorktrees(ctx, s.graph.root)
	if err != nil {
		return nil, err
	}
	var match *worktree
	for i := range list {
		if sameWorkspaceDir(list[i].path, target) {
			match = &list[i]
			break
		}
	}
	if match == nil {
		return nil, fmt.Errorf("root %s is not a worktree of this repository — a server may only act in the worktrees of the repository it was started in.\n\n%s",
			target, describeWorkspaces(s.graph.root, list))
	}

	// git prints the resolved path; opening under that spelling keeps one
	// graph per worktree however the caller spelled it.
	if hit := s.graphs.lookup(match.path); hit != nil {
		return hit, nil
	}

	g, err := s.openWorkspaceGraph(ctx, match.path)
	if err != nil {
		return nil, fmt.Errorf("could not open %s: %w", match.path, err)
	}
	s.graphs.add(g)
	for _, evicted := range s.graphs.evict(s.graph) {
		// Closing waits for each language server to shut down, which is not
		// work the caller of an unrelated tool should wait through. Close is
		// guarded by a sync.Once and the manager refuses use once closed, so
		// an in-flight call from the evicted graph degrades to "no language
		// server" — the answer ARNO already labels approximate — rather than
		// racing.
		go evicted.close()
	}
	return g, nil
}

// graphForCall picks the workspace a single tool call acts in. With no root
// argument that is the session's default workspace, so every existing client
// behaves exactly as it did before this argument existed.
func (s *mcpServer) graphForCall(ctx context.Context, args map[string]interface{}) (*workspaceGraph, error) {
	target := strings.TrimSpace(stringArg(args, "root"))
	if target == "" || sameWorkspaceDir(target, s.graph.root) {
		return s.graph, nil
	}
	return s.graphForRoot(ctx, target)
}

// noteDefaultWorktree names the worktree a mutating call landed in when the
// caller gave no root and the repository has more than one worktree: the case
// where an agent in its own worktree edits the server's by mistake. The caller
// sees the wrong path in the same turn instead of in someone's git status.
func (s *mcpServer) noteDefaultWorktree(g *workspaceGraph, tool string, args map[string]interface{}, result mcpToolResult) mcpToolResult {
	if strings.TrimSpace(stringArg(args, "root")) != "" || toolsWithoutRoot[tool] {
		return result
	}
	if hints, ok := toolAnnotationsByName[tool]; !ok || hints.ReadOnlyHint != nil {
		return result
	}
	list, err := gitWorktrees(s.ctx, g.root)
	if err != nil || len(list) < 2 {
		return result
	}
	result.Content = append(result.Content, mcpTextContent{
		Type: "text",
		Text: "acted in " + g.root + " (the server's default worktree; pass root=<path> to act in another)",
	})
	return result
}

// suggestRootForOutsidePath turns the containment refusal into an instruction
// when the path the caller named is in a sibling worktree of this repository.
//
// That refusal is issue #4's second report: a session pinned to the main
// checkout was handed an absolute path into a per-PR worktree and told only
// that it was outside the workspace, which is true and useless. The path is
// reachable — it just has to be named as a root and a path within it.
func (s *mcpServer) suggestRootForOutsidePath(err error, args map[string]interface{}) error {
	if err == nil || !errors.Is(err, pathguard.ErrOutsideWorkspace) {
		return err
	}
	target := strings.TrimSpace(stringArg(args, "path"))
	// Only an absolute path can point at another worktree; a relative one is
	// outside the workspace for an unrelated reason, such as a ../ escape.
	if target == "" || !filepath.IsAbs(target) {
		return err
	}

	list, listErr := gitWorktrees(s.ctx, s.graph.root)
	if listErr != nil {
		return err
	}
	resolved := resolveMissingPath(target)
	for _, w := range list {
		if w.current {
			continue
		}
		rel, relErr := filepath.Rel(resolveDir(w.path), resolved)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		return fmt.Errorf("%w\n\n%s is in %s, another worktree of this repository. Act there with root=%s and path=%s, which leaves this session's workspace where it is.",
			err, target, w.path, w.path, rel)
	}
	return err
}

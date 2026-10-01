package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/julianbei/arno/internal/events"
	"github.com/julianbei/arno/internal/jobs"
	"github.com/julianbei/arno/internal/languages"
)

// newWorktreeTestServer wires a server at root the way main does, so a tool
// call in a test travels the same path a client's does: handleToolCall
// resolves the workspace, then dispatch acts in it.
func newWorktreeTestServer(t *testing.T, root string) *mcpServer {
	t.Helper()
	ctx := context.Background()
	bus := events.NewBus()
	s := &mcpServer{
		bus:   bus,
		jobs:  jobs.NewRunner(bus),
		langs: languages.NewRegistry(),
		ctx:   ctx,
	}
	graph, err := s.openWorkspaceGraph(ctx, root)
	if err != nil {
		t.Fatalf("opening %s: %v", root, err)
	}
	s.graphs = newGraphCache(maxOpenWorkspaces())
	s.graphs.add(graph)
	s.graph = graph
	s.api = graph.api
	s.telemetry = graph.rec
	s.instructions = graph.instructions
	t.Cleanup(func() { s.graphs.closeAll() })
	return s
}

// The argument has to be visible in tools/list or an agent will not use it,
// which was the whole reason #4 stayed open: the capability existed via the
// workspace tool and no caller reached for it.
func TestRootArgumentIsDeclaredWhereItApplies(t *testing.T) {
	for _, tool := range tools() {
		properties, _ := schemaShape(tool.InputSchema)
		_, declared := properties["root"]

		if toolsWithoutRoot[tool.Name] {
			if declared {
				t.Errorf("%s declares root, but a root means nothing for it", tool.Name)
			}
			continue
		}
		if !declared {
			t.Errorf("%s does not declare root, so no caller can act in another worktree with it", tool.Name)
		}
	}
}

// The bug #4 reports: two callers sharing one server must be able to write to
// two worktrees. This is that, as a tool call rather than a switch.
func TestCallWithRootWritesIntoThatWorktreeOnly(t *testing.T) {
	main, side := twoWorktrees(t)
	s := newWorktreeTestServer(t, main)

	text, err := callText(t, s, "arno.create_file", map[string]interface{}{
		"root":    side,
		"path":    "only-in-side.txt",
		"content": "side\n",
	})
	if err != nil {
		t.Fatalf("a rooted create_file should succeed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(side, "only-in-side.txt")); err != nil {
		t.Fatalf("the file should have been written to the named worktree: %v\n%s", err, text)
	}
	if _, err := os.Stat(filepath.Join(main, "only-in-side.txt")); err == nil {
		t.Fatal("the file was written to the session's default workspace, which is exactly the bug")
	}
	// An edit names the directory it wrote to (0.0.13); with a root argument
	// that line is the only way a caller can tell the two apart.
	if !strings.Contains(text, filepath.Base(side)) {
		t.Errorf("the response should name the worktree it wrote to, got:\n%s", text)
	}
}

// A rooted call must not become a switch by the back door: the next call that
// names no root still acts where the session started.
func TestDefaultWorkspaceSurvivesARootedCall(t *testing.T) {
	main, side := twoWorktrees(t)
	s := newWorktreeTestServer(t, main)

	if _, err := callText(t, s, "arno.create_file", map[string]interface{}{
		"root": side, "path": "in-side.txt", "content": "side\n",
	}); err != nil {
		t.Fatalf("rooted create_file: %v", err)
	}
	if _, err := callText(t, s, "arno.create_file", map[string]interface{}{
		"path": "in-main.txt", "content": "main\n",
	}); err != nil {
		t.Fatalf("unrooted create_file: %v", err)
	}

	if _, err := os.Stat(filepath.Join(main, "in-main.txt")); err != nil {
		t.Errorf("the unrooted call should still act in the default workspace: %v", err)
	}
	if !sameWorkspaceDir(s.graph.root, main) {
		t.Errorf("the default workspace moved to %s; a rooted call must not switch the session", s.graph.root)
	}
}

// The containment rule survives the new argument. Without this, root would be
// a way to write anywhere on disk that pathguard refuses within a root.
func TestRootOutsideTheRepositoryIsRefused(t *testing.T) {
	main, _ := twoWorktrees(t)
	s := newWorktreeTestServer(t, main)
	outside := t.TempDir()

	_, err := s.graphForCall(context.Background(), map[string]interface{}{"root": outside})
	if err == nil {
		t.Fatal("expected a refusal for a root outside the repository")
	}
	if !strings.Contains(err.Error(), "not a worktree of this repository") {
		t.Errorf("the refusal should say why, got: %v", err)
	}
}

// Repeated calls into the same worktree must not rebuild its index and
// language servers each time, or the argument would be unusable in the loop
// it exists for.
func TestRepeatedRootedCallsReuseOneGraph(t *testing.T) {
	main, side := twoWorktrees(t)
	s := newWorktreeTestServer(t, main)

	first, err := s.graphForCall(context.Background(), map[string]interface{}{"root": side})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.graphForCall(context.Background(), map[string]interface{}{"root": side})
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Error("the second call into the same worktree built a second graph")
	}
	if got := len(s.graphs.open); got != 2 {
		t.Errorf("expected the default and the side worktree open, got %d: %v", got, s.graphs.roots())
	}
}

// A path that resolves to the default workspace is the default workspace,
// however it was spelled. On macOS git answers /private/var where the caller
// said /var, so this is the ordinary case rather than an exotic one.
func TestRootNamingTheDefaultWorkspaceOpensNothingNew(t *testing.T) {
	main, _ := twoWorktrees(t)
	s := newWorktreeTestServer(t, main)

	g, err := s.graphForCall(context.Background(), map[string]interface{}{"root": main})
	if err != nil {
		t.Fatal(err)
	}
	if g != s.graph {
		t.Error("naming the default workspace should resolve to the graph already serving it")
	}
	if got := len(s.graphs.open); got != 1 {
		t.Errorf("expected one open workspace, got %d: %v", got, s.graphs.roots())
	}
}

// The bound is what stops a fleet with sixty worktrees asking one server for
// sixty sets of language servers.
func TestGraphCacheEvictsLeastRecentlyUsedAndKeepsTheDefault(t *testing.T) {
	def := &workspaceGraph{root: "/repo"}
	second := &workspaceGraph{root: "/repo/b"}
	third := &workspaceGraph{root: "/repo/c"}

	c := newGraphCache(2)
	c.add(def)
	c.add(second)
	if dropped := c.evict(def); len(dropped) != 0 {
		t.Fatalf("nothing should be evicted at the bound, dropped %d", len(dropped))
	}

	c.add(third)
	dropped := c.evict(def)
	if len(dropped) != 1 || dropped[0] != second {
		t.Fatalf("the least recently used graph should go, dropped %+v", dropped)
	}
	if c.lookup("/repo") != def {
		t.Error("the default workspace must stay open however old it is")
	}
	if c.lookup("/repo/b") != nil {
		t.Error("the evicted workspace is still cached")
	}
}

// Use order, not insertion order, decides what goes.
func TestGraphCacheEvictsByUseNotByAge(t *testing.T) {
	def := &workspaceGraph{root: "/repo"}
	older := &workspaceGraph{root: "/repo/older"}
	newer := &workspaceGraph{root: "/repo/newer"}

	c := newGraphCache(3)
	c.add(def)
	c.add(older)
	c.add(newer)
	// Touching the older one makes the newer one the least recently used.
	c.lookup("/repo/older")

	c.max = 2
	dropped := c.evict(def)
	if len(dropped) != 1 || dropped[0] != newer {
		t.Fatalf("expected the least recently used to go, dropped %+v", dropped)
	}
}

// A cache holding only the default stays one over its bound rather than
// closing the workspace every unrooted call needs.
func TestGraphCacheNeverEvictsItsLastWorkspace(t *testing.T) {
	def := &workspaceGraph{root: "/repo"}
	c := newGraphCache(0)
	c.add(def)

	if dropped := c.evict(def); len(dropped) != 0 {
		t.Fatalf("the default must not be evicted, dropped %+v", dropped)
	}
	if c.lookup("/repo") != def {
		t.Error("the default workspace was dropped")
	}
}

func TestMaxOpenWorkspacesFallsBackOnABadValue(t *testing.T) {
	for _, value := range []string{"", "0", "-3", "many"} {
		t.Setenv(envMaxWorkspaces, value)
		if got := maxOpenWorkspaces(); got != maxOpenWorkspacesDefault {
			t.Errorf("%q should fall back to %d, got %d", value, maxOpenWorkspacesDefault, got)
		}
	}
	t.Setenv(envMaxWorkspaces, "8")
	if got := maxOpenWorkspaces(); got != 8 {
		t.Errorf("expected the configured bound, got %d", got)
	}
}

// Issue #4's second report: an absolute path into a sibling worktree was
// refused with nothing the caller could act on. The refusal stands — the
// path really is outside this workspace — but it now names the way in.
func TestOutsidePathInASiblingWorktreeSuggestsRoot(t *testing.T) {
	main, side := twoWorktrees(t)
	s := newWorktreeTestServer(t, main)

	_, err := callText(t, s, "arno.create_file", map[string]interface{}{
		"path":    filepath.Join(side, "plan", "README.md"),
		"content": "x\n",
	})
	if err == nil {
		t.Fatal("a path outside the workspace must still be refused")
	}
	if !strings.Contains(err.Error(), "root=") {
		t.Errorf("the refusal should name the way in, got: %v", err)
	}
	if !strings.Contains(err.Error(), filepath.Join("plan", "README.md")) {
		t.Errorf("the refusal should give the path relative to that worktree, got: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(side, "plan", "README.md")); statErr == nil {
		t.Error("nothing should have been written")
	}
}

// A path outside the repository entirely has no way in, so the refusal stays
// a plain refusal rather than inventing a worktree to suggest.
func TestOutsidePathWithNoWorktreeSuggestsNothing(t *testing.T) {
	main, _ := twoWorktrees(t)
	s := newWorktreeTestServer(t, main)

	_, err := callText(t, s, "arno.create_file", map[string]interface{}{
		"path":    filepath.Join(t.TempDir(), "elsewhere.md"),
		"content": "x\n",
	})
	if err == nil {
		t.Fatal("a path outside the repository must be refused")
	}
	if strings.Contains(err.Error(), "root=") {
		t.Errorf("there is no worktree to suggest, got: %v", err)
	}
}

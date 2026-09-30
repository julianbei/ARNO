package main

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/julianbei/arno/internal/code"
	"github.com/julianbei/arno/internal/diagnostics"
	"github.com/julianbei/arno/internal/edit"
	"github.com/julianbei/arno/internal/lsp"
	"github.com/julianbei/arno/internal/telemetry"
	"github.com/julianbei/arno/internal/transport/internalapi"
	"github.com/julianbei/arno/internal/workspace"
)

// workspaceGraph is everything that is bound to one workspace root. The bus,
// the job runner and the language registry are not here: they do not depend on
// the root, and keeping the bus across a switch keeps the background-job
// announcer's subscription alive rather than leaking a goroutine per switch.
type workspaceGraph struct {
	root    string
	servers *lsp.Manager
	api     *internalapi.Server
	rec     *telemetry.Recorder
	// instructions is what .arno/project.json in THIS root declares. Each
	// worktree carries its own, so it is rebuilt with the rest.
	instructions string
}

// close releases what the graph holds. Only the language servers own anything:
// internalapi.Server has no shutdown and code.Index none either. Leaking the
// servers is not hypothetical — jdtls and metals hold hundreds of megabytes
// each, and a session that switches worktrees a few times would hold one set
// per switch.
func (g *workspaceGraph) close() {
	if g != nil && g.servers != nil {
		g.servers.Close()
	}
}

// openWorkspaceGraph builds the root-bound half of the server. It mirrors the
// wiring in main so that a switch produces exactly what a fresh start would.
func (s *mcpServer) openWorkspaceGraph(ctx context.Context, root string) (*workspaceGraph, error) {
	wm := workspace.NewManager(root, s.bus)
	ci := code.NewIndex(root, s.bus)

	servers := lsp.NewManager(root)
	ci.UseLanguageServers(servers)

	ds := diagnostics.NewService(root)
	ds.UseLanguageServers(ci.LanguageServerDiagnostics)

	es := edit.NewService(wm, ci, ds, s.jobs)

	api := internalapi.NewServer(wm, ci, es, ds, s.jobs, s.langs, s.bus)
	if err := api.Start(ctx); err != nil {
		servers.Close()
		return nil, fmt.Errorf("failed to start internal api: %w", err)
	}

	return &workspaceGraph{
		root:         root,
		servers:      servers,
		api:          api,
		rec:          telemetry.New(root),
		instructions: projectInstructions(root) + capabilityBrief(ci),
	}, nil
}

// worktree is one entry of `git worktree list`.
type worktree struct {
	path   string
	branch string
	// current marks the workspace the session is serving now.
	current bool
}

// gitWorktreeTimeout bounds the discovery call. It reads git's own metadata
// rather than the working tree, so it is fast on any repository size, but a
// probe that runs while answering a tool call is never left unbounded.
const gitWorktreeTimeout = 10 * time.Second

// gitWorktrees lists the worktrees of the repository containing root, current
// one included. Every worktree of a repository lists all the others, so this
// answers the same set whichever one the session is serving — which is what
// makes switching back the same operation as switching away.
//
// A directory that is not a git repository lists nothing and reports why:
// switching is then unavailable, never unrestricted.
func gitWorktrees(ctx context.Context, root string) ([]worktree, error) {
	ctx, cancel := context.WithTimeout(ctx, gitWorktreeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "worktree", "list", "--porcelain")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s is not a git repository, or git could not list its worktrees: %w", root, err)
	}

	var (
		list    []worktree
		current worktree
	)
	flush := func() {
		if current.path != "" {
			list = append(list, current)
		}
		current = worktree{}
	}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			current.path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "branch "):
			current.branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "detached":
			current.branch = "detached HEAD"
		}
	}
	flush()

	for i := range list {
		if sameWorkspaceDir(list[i].path, root) {
			list[i].current = true
		}
	}
	sort.Slice(list, func(a, b int) bool { return list[a].path < list[b].path })
	return list, nil
}

// sameWorkspaceDir compares two directories by their resolved absolute paths, so that a
// symlinked or differently spelled path still matches the workspace it names.
func sameWorkspaceDir(a, b string) bool {
	resolve := func(p string) string {
		abs, err := filepath.Abs(p)
		if err != nil {
			return filepath.Clean(p)
		}
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			return real
		}
		return abs
	}
	return resolve(a) == resolve(b)
}

// describeWorkspaces reports the active workspace and what else this session
// may switch to. It is what the tool answers when given no target.
func describeWorkspaces(root string, list []worktree) string {
	var b strings.Builder
	fmt.Fprintf(&b, "workspace %s\n", root)
	if len(list) <= 1 {
		b.WriteString("no other worktree of this repository; nothing to switch to")
		return b.String()
	}
	b.WriteString("\nworktrees of this repository:\n")
	for _, w := range list {
		marker := "  "
		if w.current {
			marker = "* "
		}
		branch := w.branch
		if branch != "" {
			branch = " (" + branch + ")"
		}
		fmt.Fprintf(&b, "%s%s%s\n", marker, w.path, branch)
	}
	b.WriteString("\nswitch with workspace path=<one of these>")
	return b.String()
}

// switchWorkspace points the session at another worktree of the same
// repository and rebuilds everything bound to the root.
//
// The target must be one of this repository's worktrees. That is the whole
// containment rule: a session pointed at one repository cannot be talked into
// writing anywhere else on disk, which is the property pathguard gives within
// a root and this preserves across roots.
func (s *mcpServer) switchWorkspace(ctx context.Context, target string) (string, error) {
	list, err := gitWorktrees(ctx, s.graph.root)
	if err != nil {
		return "", err
	}

	if strings.TrimSpace(target) == "" {
		return describeWorkspaces(s.graph.root, list), nil
	}

	var match *worktree
	for i := range list {
		if sameWorkspaceDir(list[i].path, target) {
			match = &list[i]
			break
		}
	}
	if match == nil {
		return "", fmt.Errorf("%s is not a worktree of this repository — a session may only switch between the worktrees of the repository it was started in.\n\n%s",
			target, describeWorkspaces(s.graph.root, list))
	}
	if match.current {
		return fmt.Sprintf("already serving %s; nothing changed", match.path), nil
	}

	// Built before the old one is closed, so a failure leaves the session
	// serving the workspace it already had rather than none at all.
	next, err := s.openWorkspaceGraph(ctx, match.path)
	if err != nil {
		return "", fmt.Errorf("could not open %s, still serving %s: %w", match.path, s.graph.root, err)
	}
	previous := s.graph
	s.graph = next
	// The loop handles one request at a time, so no other call is reading
	// these while they are replaced.
	s.api = next.api
	s.telemetry = next.rec
	s.instructions = next.instructions
	previous.close()

	return fmt.Sprintf("workspace is now %s%s\nwas %s\n\nRevisions start again at r1 and checkpoints from the previous workspace are gone: they belong to the workspace that made them. The next call that needs a language server pays for starting it, and the symbol index is built again for this worktree.",
		match.path, branchSuffix(match.branch), previous.root), nil
}

func branchSuffix(branch string) string {
	if branch == "" {
		return ""
	}
	return " (" + branch + ")"
}

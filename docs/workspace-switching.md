# Switching the workspace within a session

Status: shipped in 0.0.13 as the `workspace` tool. Answers issue #3.

## The bug this comes from

ARNO resolves every path against one workspace root, fixed when the process
starts (`cmd/arno-mcp/root.go`, `rawRoot`): `--root`, else `ARNO_WORKSPACE`,
else the process's working directory. An MCP server is a long-lived separate
process, so a `cd` in the caller's shell — into a `git worktree` of the same
repository, say — never reaches it.

Edits then land in the starting root while the caller believes they are editing
the worktree. Nothing errors: `src/foo.ts` is a perfectly valid path inside the
pinned root, so `pathguard` is satisfied. The call reports success with an
accurate diff of the intended change, and `git status` in the worktree shows
nothing. The mistake surfaces later, in the other checkout's `git status`.

Issue #3 suggests resolving paths against the caller's current directory. That
cannot work: MCP carries no per-call working directory, and the server cannot
observe the client's. The workable form is to let a session change its
workspace deliberately.

## Decisions

| Question | Decision |
| --- | --- |
| Which directories may a session switch to? | Worktrees of the same repository, discovered with `git worktree list` |
| How does a call target a workspace? | Switch the session's active workspace; calls have no per-call override |
| Where is the tool advertised? | The full catalog only, never `core` |

Limiting the target set to the repository's own worktrees keeps the containment
`pathguard` exists to provide: a session pointed at one repository can never be
talked into writing somewhere else on disk. It also covers the case the issue
actually reports.

One active workspace, no per-call override, means there is exactly one answer
to "where did that write go" at any moment. A per-call argument would be a
second way to be wrong about it, and an agent that forgets it silently gets the
old behaviour back.

The core profile has 119 bytes of headroom against its 13,700-byte ceiling
(`profile_test.go`), and a tool schema is several hundred. The switch is a
session-setup action rather than part of an edit loop, so it lives in the full
catalog. Unlisted tools stay callable, so `--tools core` can still switch.

## Why a switch rebuilds almost everything

The root is not only a path prefix. It is handed to six things at startup:

- `workspace.NewManager` — the revision counter, the edit ledger, checkpoints
- `code.NewIndex` — the symbol index
- `lsp.NewManager` — language servers
- `diagnostics.NewService`
- `telemetry.New`
- `projectInstructions` — `.arno/project.json`, which each worktree has its own

Language servers settle it. `initialize` sends `rootUri`
(`internal/lsp/client.go`), so a running `gopls` has indexed the directory it
was started in and cannot answer about another. Repointing path resolution
alone would put edits in the new worktree while references, diagnostics and
rename answers still came from the old one — silent wrong answers in place of
silent wrong writes, which is worse, because a wrong write is at least
recoverable from the other checkout's diff.

So a switch tears the root-bound half down and builds it again. The first call
afterwards pays re-indexing and a cold language server; jdtls and metals are
slow to start and hundreds of megabytes each, which is why they are closed
rather than kept warm per workspace.

`events.Bus`, `jobs.Runner` and `languages.Registry` do not depend on the root
and are shared across switches. Keeping the bus means the background-job
announcer keeps its subscription and no goroutine leaks per switch.

## How it was built

`cmd/arno-mcp/workspace_switch.go` holds the graph, the discovery and the
switch; `main` builds the first graph through the same function a switch uses,
so the two paths cannot drift apart.

The MCP loop is sequential — `resp := s.handleRequest(req)`, no goroutine per
request — so a switch handler runs with no other request in flight and can
rebuild synchronously without locking the graph.

1. Extract the root-bound services into one struct built by a single function,
   with a `close` that calls `lsp.Manager.Close`. Nothing else in the graph
   holds an OS resource: `internalapi.Server` has only `Start`, and
   `code.Index` has no shutdown.
2. Hold that struct on `mcpServer` and reach the API through it (about 34
   sites use `s.api` today).
3. Discover targets with `git worktree list --porcelain` from the current root.
   Every worktree of a repository lists all the others, so switching back is
   the same operation as switching away.
4. Refuse a target that is not in that list, and say what the valid ones are.
   A root that is not a git repository can list nothing, so switching is
   unavailable rather than unrestricted.
5. Report the absolute root in the switch response, and state the cost — the
   next call re-indexes and starts a language server.

## Still open

Whether an edit response should name its absolute root on every call, not only
after a switch. It is the part of issue #3 that makes the original failure
visible rather than preventable, and it costs bytes in a response an agent
reads constantly.

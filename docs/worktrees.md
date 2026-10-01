# Working in several git worktrees

One ARNO server can act in any worktree of the repository it was started in.
A tool call names the worktree it wants with `root`; calls that name none act
in the workspace the server was started in, exactly as before.

```
arno.grep        query=parseHeader                             # the session's workspace
arno.grep        query=parseHeader  root=/repos/wt-spell       # a sibling worktree
arno.replace_text path=src/a.ts ... root=/repos/wt-spell       # written there, not here
```

This exists because of how MCP works. A stdio server is one process per
client connection, and a request carries no caller identity, so a session and
the subagents inside it share one server and one JSON-RPC stream. Before
0.0.15 the only way to reach another worktree was `arno.workspace path=`,
which repoints the whole server: safe for one caller, silently wrong for two.
Naming the worktree per call is what lets several callers share a server
without disturbing each other.

## The rules

**Only this repository's worktrees.** A `root` is checked against
`git worktree list` for the repository the server was started in. Anything
else is refused and the refusal lists the valid targets. This is the same
containment rule `pathguard` gives inside a root, extended across roots: a
server pointed at one repository cannot be talked into writing anywhere else
on disk.

**Each worktree is a full workspace.** Its own symbol index, language
servers, revision counter, checkpoints and `.arno/`. A revision number or a
checkpoint from one worktree means nothing in another, because they describe
different trees. Telemetry is recorded in the root the call acted in.

**At most three open at once**, least recently used evicted first, the
session's default never evicted. Set `ARNO_MAX_WORKSPACES` to change the
bound. The bound exists because a workspace is not free: each one starts its
own language servers, and jdtls or rust-analyzer on a large repository is
hundreds of megabytes. A fleet with sixty per-PR worktrees must not be able
to ask one server for sixty sets of them.

Eviction closes language servers; it never touches files or your git state.
The next call into an evicted worktree reopens it, paying for the index and
the language server start again.

## Which to use

| You want | Use |
| --- | --- |
| A session that stays in one worktree | `--root` at startup, nothing else |
| One call elsewhere, everything else unchanged | `root=` on that call |
| Several callers in several worktrees at once | `root=` on every call |
| A session that has moved to another worktree for good | `arno.workspace path=` |

`arno.workspace` with no path lists the worktrees and marks the default. It
remains the way to change the default; it is no longer the way to reach
another worktree for a single call.

## A path in the wrong worktree

An absolute path into a sibling worktree is still refused — it really is
outside this workspace — but the refusal names the way in:

```
outside the workspace: /repos/wt-spell/plan/README.md is not inside the
workspace root /repos/main

/repos/wt-spell/plan/README.md is in /repos/wt-spell, another worktree of
this repository. Act there with root=/repos/wt-spell and path=plan/README.md,
which leaves this session's workspace where it is.
```

## One server per worktree

Still supported, and still the right answer when each worktree is long-lived
and belongs to a different agent:

```json
{"mcpServers": {
  "arno-main":  {"command": "arno-mcp", "args": ["--root", "/repos/main",     "--tools", "core"]},
  "arno-spell": {"command": "arno-mcp", "args": ["--root", "/repos/wt-spell", "--tools", "core"]}
}}
```

Each process is fully isolated and nothing is shared or evicted. It does not
work for worktrees created during a session — a per-PR scratch worktree
cannot be in a config written before it existed — which is the case `root`
answers.

## What this does not do

- **Across repositories.** A server serves one repository. Two repositories
  are two servers.
- **Concurrently within one server.** The MCP loop handles one request at a
  time, so two rooted calls are serialised like any other two calls. What
  changes is that they no longer corrupt each other's view of the tree.
- **Share checkpoints or revisions between worktrees.** They are per
  workspace and always were.

## Implementation

`cmd/arno-mcp/workspace_cache.go`. `graphForCall` picks the workspace for one
call; `graphForRoot` validates against `git worktree list` and serves from the
cache, opening and evicting as needed. Everything bound to a root lives in a
`workspaceGraph` (`cmd/arno-mcp/workspace_switch.go`), which is also what the
workspace switch swaps, so the two paths cannot drift apart. Dispatch reads
`g.api` rather than the server's, which is the whole reason two calls can act
in two trees.

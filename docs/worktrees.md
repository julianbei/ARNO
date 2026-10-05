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
| A session that stays in one worktree | `arno.workspace path=<it>` once; its subagents inherit that |
| One call elsewhere, everything else unchanged | `root=` on that call |
| A subagent working in a different worktree than its parent | `root=` in that subagent's brief, on every call |
| A server started in the right worktree | `--root` at startup |

`arno.workspace` with no path lists the worktrees and marks the default.

Every Claude session starts its own server, usually from the shared main
checkout, so an undeclared session's default is the checkout every other
session also defaults to. Subagents share their parent's server and so its
declared workspace; the server cannot tell them apart, which is why a subagent
in another worktree has to name it.

## Strict mode

`ARNO_REQUIRE_WORKSPACE=1` makes the server refuse any call that can change
something — edits, creates, deletes, `run_command`, `revert` and the rest —
until the session has said which worktree it works in. It applies only in a
repository with more than one worktree; reads are never refused.

```
arno.create_file not run: this repository has 3 worktrees and this session has
not said which one it works in, so the change would land in /repos/main, the
server's start worktree, which may not be yours. Nothing was written.

Say it once with workspace path=<your worktree> — after that no call needs
root — or pass root=<your worktree> on this call.
```

Naming the worktree the server started in counts: a session that really is
working there has nothing to switch, but strict mode still wants it said once.
Listing the worktrees does not count. `root=` on a call satisfies that call
only and does not declare the session's own worktree.

It is off by default. A repository with a stray second worktree and a session
working in the first is the ordinary case, and refusing it would punish
everyone for a failure only a fleet of worktrees has: undeclared sessions all
default to the same checkout, so one that forgot to move lands its writes on
top of everyone else's, with no error.

What it does not catch: a subagent working in a different worktree from its
parent without `root=` writes into the parent's declared worktree. The edit
response ends with `acted in <path> (this session's declared worktree; …)` so
the wrong tree shows up in the same turn.

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

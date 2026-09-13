# Tool surface contract — 0.0.1

This is what jade promises to callers, and what it does not.

## Why this is frozen at all

The MCP tool catalog is fixed at connection time. A client holds whatever
surface it saw when it connected, so tool names and required arguments become a
published interface the moment anyone integrates — and an accidental change is
invisible until someone else's session breaks, in a way that looks like their
bug rather than ours.

Two practical consequences:

- **A new tool does not appear until the client reconnects.** Not a bug; it is
  how the protocol works. Every "brand-new tool is missing" report in this
  project's own development turned out to be this.
- **Renaming a tool is not a rename.** Old clients keep calling the old name
  until they reconnect, so from their side it is a deletion.

## Enforcement

`cmd/jade-mcp/contract_test.go` holds the frozen list and fails on any drift.
It is deliberately a golden list rather than a generated one: the point is to
make a change *deliberate*, and a test that regenerates its own expectation
cannot do that.

`make test` runs it. `run_command docs-check` separately verifies every served
tool appears in the README.

## Stability policy

| Change | Compatible? | What callers see |
|---|---|---|
| Add a tool | Yes | Nothing, until they reconnect |
| Add an optional argument | Yes | Nothing |
| Make a required argument optional | Yes | Nothing |
| Change response *wording* | Yes | Text differs; see below |
| Remove or rename a tool | **No** | Calls fail at the next call |
| Make an optional argument required | **No** | Calls fail at the next call |
| Change a response's *structure* | **No** | Parsing breaks |

Breaking changes need a version bump and a line in the release notes.

## Required arguments are a promise the server enforces

A required argument the implementation ignores is worse than either choice
alone: a client that trusts the schema sends a value it never needed, and one
that does not is told it is wrong when it is not.

This was true of `expectedRevision` on `replace_text` and `replace_range`,
which declared it required while the server treated an empty value as "no
precondition" — the same as `apply` and `replace_symbol`. Fixed at the freeze
by making the schema match the behaviour, since the behaviour was the right
one: a one-shot edit should not have to fetch a revision first.

`expectedRevision` remains **optional and recommended** everywhere it appears.
Supplying it turns a lost-update race into a clean `stale revision` rejection.

## Response format

Responses are **plain text, not JSON**, shaped to lead with the decisive line.
See [response-style.md](response-style.md) for the rules and the benchmark that
motivated them (JSON cost 5.63x the tokens of the equivalent shell commands;
plain text brought it to 0.85x).

What is promised:

- The first line carries the verdict or the identifier a caller needs next —
  `pass tests`, `r7 · 3 files, +40 -12`, `not found: X`, `ambiguous: X matches …`.
- Failures are stated, never implied by absence.
- Truncation is always announced, with the true total (`… 48 more files`).
- Line-oriented output stays `path:line: text`, which is greppable and
  diffable.

What is **not** promised: exact wording. Treat responses as text for a model to
read, not as a format to parse. `JADE_JSON=1` switches every response to JSON
if you need something machine-readable; that JSON mirrors the Go structs in
`internal/protocol/types.go` and is subject to the same stability table above.

## Known asymmetries, deliberately kept

- `delete_symbol` and `read_symbol` require `path` but accept *either*
  `symbolName` or `symbolId`. JSON Schema's `required` cannot express
  "one of these two", so the server enforces it and returns a clear error.
- `create_file` refuses to overwrite; `replace_file` refuses to create. Neither
  takes a force flag — choosing the tool is the explicit act.
- `search` ranks declaration *names*; `grep` searches *text*; `find` does both
  and returns bodies. They overlap and that is intended, but `search` will not
  find a struct field or a string literal. Its description says so.

## The frozen surface

34 tools. `cmd/jade-mcp/contract_test.go` is the authority; this table is for
reading.

| Tool | Required arguments |
|---|---|
| `jade.apply` | `edits` |
| `jade.changes` | — |
| `jade.check` | — |
| `jade.checkpoint` | — |
| `jade.context` | `path` |
| `jade.create_file` | `content`, `path` |
| `jade.declare_command` | `name` |
| `jade.delete_file` | `path` |
| `jade.delete_symbol` | `path` |
| `jade.diff` | — |
| `jade.events` | — |
| `jade.find` | — (`query` or `queries`, enforced by the server) |
| `jade.grep` | — (`query` or `queries`, enforced by the server) |
| `jade.history` | `path` |
| `jade.insert` | `path`, `text` |
| `jade.job_output` | `id` |
| `jade.job_status` | `id` |
| `jade.outline` | `path` |
| `jade.read_range` | — (`path` or `ranges`, enforced by the server) |
| `jade.read_symbol` | `path` |
| `jade.references` | `path` |
| `jade.rename` | `newName`, `path` |
| `jade.replace_file` | `content`, `path` |
| `jade.replace_range` | `endLine`, `newCode`, `path`, `startLine` |
| `jade.replace_symbol` | `newCode`, `symbolId` |
| `jade.replace_text` | `newText`, `oldText`, `path` |
| `jade.repository_map` | `query` |
| `jade.retrieve` | `query` |
| `jade.revert` | `checkpointId` |
| `jade.run_command` | — |
| `jade.run_tests` | — |
| `jade.search` | `query` |
| `jade.search_nudge` | `command` |
| `jade.telemetry` | — |
| `jade.workspace_tree` | — |

## Budgets and provenance — 0.0.5 design

*Implemented in 0.0.5 for `grep`, `find`, `references`, `read_range` (and its
`ranges`), `read_symbol`, `workspace_tree`, `history`, `diff` and
`job_output`; `cmd/jade-mcp/contract_test.go` holds each to it.* `outline`
stays whole — a file's declarations are a short list. `events` keeps its
`after` cursor, which already continues. Two conventions that every list- or
body-returning response will share. Both are additive: new optional
arguments and new response lines, so neither breaks a caller. The per-tool
size arguments they replace are deprecated in 0.0.5 and removed before 0.1.0.

### Budget and continuation

Today each tool has its own size argument — `limit` on `grep`, `find`,
`search`, `history` and `events`, `maxLines` on `find` and `read_symbol` —
and `read_range` cuts silently at 20,000 bytes in the middle of the file.
The replacement:

- **`budget`**, an optional integer in tokens, on every tool that returns a
  list or a body: `grep`, `find`, `search`, `retrieve`, `references`,
  `outline`, `read_range`, `read_symbol`, `workspace_tree`, `repository_map`,
  `context`, `history`, `events`, `changes`, `diff`, `job_output`. Tokens are
  counted as bytes / 4, the estimate the benchmark already uses. Each tool
  keeps today's effective default; a server-wide cap bounds the largest
  budget.
- **Cut at whole items**: a match, a declaration, a file entry, a line of a
  range. Never mid-line, never mid-body without saying so.
- **One closing line when cut**, with the true total and a handle:
  `shown 24 of 87 references · continue=c3f9a1`.
- **`continue`**, an optional string argument on the same tools: the same
  call's next page, under the same or a new `budget`. Other arguments are
  ignored when `continue` is given.
- **Handles are opaque and server-side**, kept per session, least recently
  used first out. A handle names the revision it was cut at; after the
  workspace moves, it is refused with both revisions named
  (`continue=c3f9a1 was cut at r12; the workspace is at r14 — repeat the
  call`), never answered from a different state.
- **`limit` and `maxLines` stay accepted** through 0.0.x and are read as a
  budget in items or lines. `context` on `grep` is not a size argument and
  stays.

### Provenance

An answer that can come from more than one backend, or be incomplete, says
which on one compact line — `exact · gopls · complete`. Three parts, each a
closed set defined by core. A backend reports its class and its limits;
core renders the words, so no backend chooses its own confidence wording.

| Part | Values |
|---|---|
| Certainty | `exact` (a compiler, language server or literal text match), `structural` (a tree-sitter parse), `approximate` (name matching in the text index), `text fallback` (heuristic scan, no grammar) |
| Source | the backend that answered: `gopls`, `rust-analyzer`, `tree-sitter`, `text index`, … — never a tool name or argument |
| Completeness | `complete`, `cut` (by the budget; a `continue` handle follows), `may be incomplete` (server still indexing, files skipped), `parse errors`, `stale` |

Where it goes: appended to the first line after ` · `, so the verdict or
identifier still leads — `12 references to Store · exact · gopls · complete`.
Tools whose answer has one possible source and cannot be partial — an exact
`read_range` within budget, `diff`, `checkpoint` — carry no provenance.
Edit responses keep `checked: <source>`, using the same source names.

In `JADE_JSON=1` output both conventions are new fields — `Provenance`
(`Certainty`, `Source`, `Completeness`) and `Continue` — added to the
response structs, not replacing any.

### Order of work

1. Provenance types and rendering in core; `references`, `find`, `outline`
   and `grep` first, since they already distinguish exact from approximate.
2. The continuation store and `budget` on `grep`, `find`, `references` and
   `read_range`, the tools the benchmark agents called most.
3. The remaining tools; `limit` and `maxLines` marked deprecated in their
   schema descriptions.
4. Contract tests: every list-returning tool accepts `budget` and
   `continue`; every provenance value comes from the closed sets.

## What is explicitly not frozen

- **Response wording**, as above.
- **`.jade/commands.json` and `.jade/telemetry.jsonl` formats.** Both are
  local files, neither is a wire format, and telemetry in particular is
  expected to grow fields.
- **Symbol ID spelling** (`path::Name@line`). Treat it as opaque and obtain it
  from a response rather than constructing it.
- **Anything under `internal/`.** Go's own visibility rules already say so.
